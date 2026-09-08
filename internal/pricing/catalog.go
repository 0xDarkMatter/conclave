// Package pricing keeps a locally cached copy of the OpenRouter model catalog
// (https://openrouter.ai/api/v1/models) and answers two questions for the rest
// of conclave: "does this model still exist?" and "what does it cost per token?".
//
// Contract:
//   - The catalog is ADVISORY. Every caller must work when it is nil. A network
//     failure, a missing cache, or CONCLAVE_NO_PRICING=1 must never fail a query.
//   - Prices are pay-as-you-go API prices in USD per million tokens. CLI-mode
//     providers ride subscriptions, so callers must only present these numbers
//     as the cost of API mode (-g / -c / --batch). See docs/MODEL_REGISTRY.md.
//   - OpenRouter is used because it needs no auth and covers every vendor
//     conclave talks to. It is a proxy for the vendor's own list: a model missing
//     here is a strong hint, not proof, that the vendor retired it. Hence warnings,
//     never hard errors, on a miss.
//   - The cache lives at $XDG_CACHE_HOME/conclave/openrouter-models.json (or
//     ~/.cache/conclave/). Freshness is CONCLAVE_PRICING_TTL hours (default 24).
//     A stale cache is served immediately and refreshed in the background so no
//     query ever blocks on the network once a cache exists; only the very first
//     run (or --refresh) fetches synchronously, bounded by fetchTimeout.
//
// Decision record: docs/adr/ADR-009-runtime-pricing-catalog-from-openrouter.md
package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the unauthenticated OpenRouter models feed.
const DefaultURL = "https://openrouter.ai/api/v1/models"

const (
	cacheFileName = "openrouter-models.json"
	defaultTTL    = 24 * time.Hour
	// fetchTimeout bounds the ONE synchronous fetch a user can hit (first run or
	// --refresh). Kept short on purpose: a slow feed must not make conclave feel
	// slow. Background refreshes use the same bound.
	fetchTimeout = 6 * time.Second
	// maxBody guards against a runaway response; the feed is ~1 MB today.
	maxBody = 16 << 20
)

// Model is one entry of the catalog, normalised to conclave's units.
type Model struct {
	// ID is the OpenRouter slug, e.g. "anthropic/claude-opus-4.8".
	ID string `json:"id"`
	// Name is OpenRouter's human label, e.g. "Anthropic: Claude Opus 4.8".
	Name string `json:"name"`
	// ContextLength in tokens.
	ContextLength int `json:"context_length"`
	// InputPerM / OutputPerM are USD per million tokens. OpenRouter serves
	// per-token strings; we convert once on ingest.
	InputPerM  float64 `json:"input_per_m"`
	OutputPerM float64 `json:"output_per_m"`
	// Created is the vendor release timestamp OpenRouter reports.
	Created time.Time `json:"created"`
}

// Vendor returns the slug's vendor prefix ("anthropic" for "anthropic/x").
func (m Model) Vendor() string {
	if i := strings.IndexByte(m.ID, '/'); i > 0 {
		return m.ID[:i]
	}
	return ""
}

// Slug returns the slug without the vendor prefix.
func (m Model) Slug() string {
	if i := strings.IndexByte(m.ID, '/'); i > 0 {
		return m.ID[i+1:]
	}
	return m.ID
}

// Catalog is the cached, queryable model list.
type Catalog struct {
	FetchedAt time.Time `json:"fetched_at"`
	Source    string    `json:"source"`
	Models    []Model   `json:"models"`

	// Stale is true when this catalog was served from a cache older than the
	// TTL (a background refresh may be in flight). Not persisted.
	Stale bool `json:"-"`

	byID map[string]Model
}

// vendorPrefix maps a conclave provider name to the OpenRouter vendor prefix.
// This is the ONLY place that mapping lives; keep it in sync with
// providers.AllAPIProviders when a provider is added.
var vendorPrefix = map[string]string{
	"gemini":     "google",
	"openai":     "openai",
	"claude":     "anthropic",
	"perplexity": "perplexity",
	"grok":       "x-ai",
	"glm":        "z-ai",
}

// VendorPrefix exposes the provider → OpenRouter vendor mapping (read-only use).
// A slash-routed OpenRouter token ("deepseek/deepseek-v4", ADR-010) is its own
// vendor prefix ("deepseek"), so drift warnings and "newest listed" hints work
// for any vendor in the feed without a map entry.
func VendorPrefix(provider string) (string, bool) {
	return vendorFor(provider)
}

// isSlashToken mirrors providers.IsOpenRouterModel (both halves of
// "vendor/model" non-empty) without importing it: the pricing package must
// stay free of a providers dependency. Change both together.
func isSlashToken(provider string) bool {
	i := strings.IndexByte(provider, '/')
	return i > 0 && i < len(provider)-1
}

func vendorFor(provider string) (string, bool) {
	if isSlashToken(provider) {
		return provider[:strings.IndexByte(provider, '/')], true
	}
	v, ok := vendorPrefix[provider]
	return v, ok
}

// Options controls Load.
type Options struct {
	// URL overrides the feed (tests). Empty means DefaultURL or
	// $CONCLAVE_OPENROUTER_MODELS_URL.
	URL string
	// CacheDir overrides the cache directory (tests). Empty means the XDG path.
	CacheDir string
	// TTL overrides freshness. Zero means $CONCLAVE_PRICING_TTL hours or 24h.
	TTL time.Duration
	// ForceRefresh ignores the cache and fetches synchronously.
	ForceRefresh bool
	// Offline never touches the network: return the cache (however old) or nil.
	Offline bool
	// Client overrides the HTTP client (tests).
	Client *http.Client
}

// background tracks in-flight refresh goroutines so the CLI can give them a
// moment to finish writing the cache before the process exits.
var background sync.WaitGroup

// WaitBackground blocks until any background refresh completes or the timeout
// elapses. Call once at the end of a command; never before the real work.
func WaitBackground(timeout time.Duration) {
	done := make(chan struct{})
	go func() { background.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// Disabled reports whether the user switched the catalog off entirely.
func Disabled() bool {
	v := strings.TrimSpace(os.Getenv("CONCLAVE_NO_PRICING"))
	return v != "" && v != "0" && !strings.EqualFold(v, "false")
}

// Load returns the best catalog available under the rules in the package doc.
// The returned error is informational (callers log it at most); a nil catalog
// with a nil error means the feature is disabled.
func Load(ctx context.Context, opts Options) (*Catalog, error) {
	if Disabled() {
		return nil, nil
	}
	ttl := opts.TTL
	if ttl == 0 {
		ttl = ttlFromEnv()
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = defaultCacheDir()
	}
	cachePath := filepath.Join(cacheDir, cacheFileName)

	cached, cacheErr := readCache(cachePath)

	if opts.Offline {
		if cached != nil {
			cached.Stale = time.Since(cached.FetchedAt) > ttl
		}
		return cached, cacheErr
	}

	fresh := cached != nil && time.Since(cached.FetchedAt) <= ttl
	if fresh && !opts.ForceRefresh {
		return cached, nil
	}

	// Stale-but-present: serve it now, refresh behind the user's back.
	if cached != nil && !opts.ForceRefresh {
		cached.Stale = true
		background.Add(1)
		go func() {
			defer background.Done()
			bctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
			defer cancel()
			if c, err := fetch(bctx, opts); err == nil {
				_ = writeCache(cachePath, c)
			}
		}()
		return cached, nil
	}

	// No cache (or forced): the one synchronous fetch.
	fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	c, err := fetch(fctx, opts)
	if err != nil {
		if cached != nil {
			cached.Stale = true
			return cached, fmt.Errorf("pricing refresh failed, using cache from %s: %w", cached.FetchedAt.Format("2006-01-02"), err)
		}
		return nil, fmt.Errorf("pricing catalog unavailable: %w", err)
	}
	if werr := writeCache(cachePath, c); werr != nil {
		return c, fmt.Errorf("pricing cache not written: %w", werr)
	}
	return c, nil
}

// CachePath returns where Load reads/writes the cache (for --verbose output).
func CachePath() string {
	return filepath.Join(defaultCacheDir(), cacheFileName)
}

// === Lookup ===

// Lookup finds the catalog entry for a conclave provider + vendor-native model
// id. It tolerates the naming drift between vendor ids and OpenRouter slugs:
//
//	claude-opus-4-8              -> anthropic/claude-opus-4.8
//	claude-haiku-4-5-20251001    -> anthropic/claude-haiku-4.5   (date suffix dropped)
//	claude-fable-5-1             -> anthropic/claude-fable-5.1
//	zai-coding-plan/glm-5.2      -> z-ai/glm-5.2                 (foreign prefix dropped)
//	grok-4-1-fast-reasoning      -> x-ai/grok-4.1-fast           (variant suffix dropped)
//
// Exact slug matches always win; the rewrites are only tried on a miss.
//
// For a slash-routed OpenRouter token the provider name IS the catalog id, so
// it is looked up verbatim (then the model, in case a -m override differs).
// No rewriting: the user typed an OpenRouter slug, not a vendor id.
func (c *Catalog) Lookup(provider, model string) (Model, bool) {
	if c == nil {
		return Model{}, false
	}
	if isSlashToken(provider) {
		for _, id := range []string{provider, model} {
			if m, ok := c.byID[id]; ok {
				return m, true
			}
		}
		return Model{}, false
	}
	vendor, ok := vendorPrefix[provider]
	if !ok {
		return Model{}, false
	}
	for _, cand := range candidateSlugs(model) {
		if m, ok := c.byID[vendor+"/"+cand]; ok {
			return m, true
		}
	}
	return Model{}, false
}

// NameOf returns OpenRouter's human label for an exact slug ("DeepSeek: DeepSeek
// V4"). Nil-safe; used as the providers.DisplayName hook for slash tokens.
func (c *Catalog) NameOf(id string) (string, bool) {
	if c == nil {
		return "", false
	}
	m, ok := c.byID[id]
	if !ok || m.Name == "" {
		return "", false
	}
	return m.Name, true
}

// Has is Lookup without the payload.
func (c *Catalog) Has(provider, model string) bool {
	_, ok := c.Lookup(provider, model)
	return ok
}

// Price returns (input, output) USD per million tokens, or ok=false.
func (c *Catalog) Price(provider, model string) (in, out float64, ok bool) {
	m, ok := c.Lookup(provider, model)
	if !ok {
		return 0, 0, false
	}
	return m.InputPerM, m.OutputPerM, true
}

// ByVendor returns the models for one conclave provider, newest first,
// excluding OpenRouter-only variants (":free", ":batch", ":thinking" ...).
func (c *Catalog) ByVendor(provider string) []Model {
	if c == nil {
		return nil
	}
	vendor, ok := vendorFor(provider)
	if !ok {
		return nil
	}
	var out []Model
	for _, m := range c.Models {
		if m.Vendor() != vendor || strings.Contains(m.Slug(), ":") {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Providers lists the conclave provider names this package knows, sorted.
func Providers() []string {
	names := make([]string, 0, len(vendorPrefix))
	for k := range vendorPrefix {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

var (
	// trailing -YYYYMMDD (Anthropic dated ids)
	reDateSuffix = regexp.MustCompile(`-\d{8}$`)
	// "-4-8" / "-5-1" version pairs that OpenRouter writes as "4.8" / "5.1"
	// (RE2 has no lookahead, so the trailing boundary is captured and re-emitted)
	reDashVersion = regexp.MustCompile(`-(\d+)-(\d+)(-|$)`)
	// xAI reasoning variants collapse to the base model on OpenRouter
	reGrokVariant = regexp.MustCompile(`-(non-reasoning|reasoning)$`)
)

// candidateSlugs yields the model slugs to try, most literal first. Exported
// only through Lookup; kept pure so it is trivially testable.
func candidateSlugs(model string) []string {
	model = strings.TrimSpace(model)
	if i := strings.LastIndexByte(model, '/'); i >= 0 {
		model = model[i+1:] // "zai-coding-plan/glm-5.2" -> "glm-5.2"
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(model)
	noDate := reDateSuffix.ReplaceAllString(model, "")
	add(noDate)
	dotted := reDashVersion.ReplaceAllString(noDate, "-$1.$2$3")
	add(dotted)
	add(reGrokVariant.ReplaceAllString(dotted, ""))
	return out
}

// === Fetch / cache ===

// wire mirrors the subset of OpenRouter's response we keep.
type wire struct {
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Created       int64  `json:"created"`
		ContextLength int    `json:"context_length"`
		Pricing       struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

func fetch(ctx context.Context, opts Options) (*Catalog, error) {
	url := opts.URL
	if url == "" {
		url = os.Getenv("CONCLAVE_OPENROUTER_MODELS_URL")
	}
	if url == "" {
		url = DefaultURL
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "conclave-cli (+https://github.com/0xDarkMatter/conclave)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	var w wire
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("decode models feed: %w", err)
	}
	if len(w.Data) == 0 {
		return nil, errors.New("models feed returned no models")
	}
	c := &Catalog{FetchedAt: time.Now().UTC(), Source: url}
	for _, d := range w.Data {
		in, _ := strconv.ParseFloat(d.Pricing.Prompt, 64)
		out, _ := strconv.ParseFloat(d.Pricing.Completion, 64)
		c.Models = append(c.Models, Model{
			ID:            d.ID,
			Name:          d.Name,
			ContextLength: d.ContextLength,
			InputPerM:     in * 1_000_000,
			OutputPerM:    out * 1_000_000,
			Created:       time.Unix(d.Created, 0).UTC(),
		})
	}
	c.index()
	return c, nil
}

func (c *Catalog) index() {
	c.byID = make(map[string]Model, len(c.Models))
	for _, m := range c.Models {
		c.byID[m.ID] = m
	}
}

func readCache(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("corrupt pricing cache %s: %w", path, err)
	}
	if len(c.Models) == 0 {
		return nil, nil
	}
	c.index()
	return &c, nil
}

// writeCache is atomic (temp file + rename) so a concurrent conclave never
// reads a half-written cache.
func writeCache(path string, c *Catalog) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".models-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// Windows refuses to rename over an existing file in some configurations;
	// removing first is the portable choice and the window is harmless (reader
	// falls back to "no cache" and refetches).
	_ = os.Remove(path)
	return os.Rename(tmpName, path)
}

func defaultCacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "conclave")
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "conclave")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "conclave")
	}
	return filepath.Join(home, ".cache", "conclave")
}

func ttlFromEnv() time.Duration {
	if v := os.Getenv("CONCLAVE_PRICING_TTL"); v != "" {
		if h, err := strconv.ParseFloat(v, 64); err == nil && h > 0 {
			return time.Duration(h * float64(time.Hour))
		}
	}
	return defaultTTL
}

// NewCatalog builds an in-memory catalog from an explicit model list and
// indexes it. Exists so callers outside this package (tests, fixtures) can
// construct a queryable catalog without a network round trip; Lookup depends
// on an index that plain json.Unmarshal does not build.
func NewCatalog(models []Model) *Catalog {
	c := &Catalog{FetchedAt: time.Now().UTC(), Source: "in-memory", Models: models}
	c.index()
	return c
}

// CacheDir is the conclave cache root ($XDG_CACHE_HOME/conclave, or the OS
// equivalent). Exported so sibling caches (internal/cache) sit beside the
// pricing cache instead of re-deriving the XDG rules and drifting from them.
func CacheDir() string { return defaultCacheDir() }

// === Costing ===
//
// This is the ONE cost engine. Before it existed, internal/output and
// internal/batch each priced responses with their own copy of the maths and
// their own copy of the judge split, and they had already diverged. Anything
// that turns tokens into dollars belongs here.

// JudgeInputShare splits a judge's single token count into input and output for
// pricing. The Provider interface reports one total (judge.Verdict carries
// JudgeTokens, not a split), and a synthesis prompt is dominated by the pasted
// provider answers, so the bias is heavily toward input.
const JudgeInputShare = 0.7

// CostOf returns the USD cost of one call. ok=false means the catalog cannot
// price this model, which callers must treat as "unknown" — never as zero.
// A nil catalog always reports ok=false, so the advisory contract holds.
func (c *Catalog) CostOf(provider, model string, inTokens, outTokens int) (cost float64, ok bool) {
	in, out, ok := c.Price(provider, model)
	if !ok {
		return 0, false
	}
	return float64(inTokens)*in/1_000_000 + float64(outTokens)*out/1_000_000, true
}

// JudgeCostOf prices a judge call from its combined token count, applying
// JudgeInputShare. Same ok semantics as CostOf.
func (c *Catalog) JudgeCostOf(provider, model string, totalTokens int) (cost float64, ok bool) {
	in, out, ok := c.Price(provider, model)
	if !ok {
		return 0, false
	}
	t := float64(totalTokens)
	return t*JudgeInputShare*in/1_000_000 + t*(1-JudgeInputShare)*out/1_000_000, true
}

// FormatUSD renders a KNOWN cost. Never call it for an unknown one: an unknown
// price must be omitted, because a printed $0.00 reads as "this was free".
// A real amount below a tenth of a cent renders as "<$0.0001" rather than
// rounding down to a zero that would read the same way.
func FormatUSD(v float64) string {
	switch {
	case v <= 0:
		return "$0.0000"
	case v < 0.0001:
		return "<$0.0001"
	default:
		return fmt.Sprintf("$%.4f", v)
	}
}
