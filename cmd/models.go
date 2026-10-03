package cmd

// `conclave models` — inspect the cached OpenRouter pricing catalog and check
// conclave's compiled defaults against it. Read-only apart from the cache
// file. See internal/pricing for the contract and ADR-009 for the decision.
//
// `conclave models --frontier` draws price-performance frontiers from
// EXTERNAL quality sources only (ADR-018); see the FRONTIER section below.
// Every source is advisory: one failing leaves its models unscored with a
// warning on stderr, and only "no source at all" exits 3, the same code as
// `models --check` without a catalog (cmd/exit.go).
//
// Sections: FLAGS, LISTING (runModels and the catalog table), FRONTIER,
// CHECK and helpers.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/benchmarks/decisionindex"
	"github.com/0xDarkMatter/conclave-cli/internal/benchmarks/openrouter"
	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/frontier"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"github.com/spf13/cobra"
)

// === FLAGS ===

var (
	flagModelsRefresh bool
	flagModelsJSON    bool
	flagModelsCheck   bool
	flagModelsAll     bool

	// --frontier family (ADR-018). --axis and --cost default to "" so the
	// mode can tell "not given" from a value: --deciders rejects any --axis.
	flagModelsFrontier bool
	flagModelsDeciders bool
	flagModelsAxis     string
	flagModelsCost     string
	flagModelsHTML     string
)

var modelsCmd = &cobra.Command{
	Use:   "models [provider]",
	Short: "Show current model ids and API prices from the OpenRouter catalog",
	Long: `Prints the models OpenRouter currently lists for each provider conclave
supports, with context length and pay-as-you-go API price (USD per million
tokens). The catalog is cached for ` + "`CONCLAVE_PRICING_TTL`" + ` hours (default 24)
under the user cache directory and refreshed in the background when stale.

Prices apply to API mode (-g / -c / --batch). CLI mode rides each provider's
subscription and pays nothing per token.

  conclave models                 # every provider, newest models first
  conclave models claude          # one provider
  conclave models clef            # one decision model (hand price table with as-of date; needs no catalog)
  conclave models --check         # verify compiled defaults still exist (exit 2 on drift, 3 if the catalog is unreachable)
  conclave models --refresh       # force a fetch now
  conclave models --json          # machine-readable dump of the cache

Price-performance frontiers from external quality sources (never evals Conclave
runs; a model with no external score is listed as unscored, never estimated):

  conclave models --frontier                       # chat: AA Intelligence Index vs blended list price (needs OPENROUTER_API_KEY)
  conclave models --frontier --axis coding --cost task
  conclave models --frontier --deciders            # decision models: Decision Index vs input list price (no key needed)
  conclave models --frontier --html report.html    # also write a self-contained HTML report
  conclave models --frontier --json                # the frontier.Result document`,
	Args: cobra.MaximumNArgs(1),
	RunE: runModels,
}

func init() {
	modelsCmd.Flags().BoolVar(&flagModelsRefresh, "refresh", false, "Fetch the catalog now instead of using the cache")
	modelsCmd.Flags().BoolVar(&flagModelsJSON, "json", false, "Print the cached catalog as JSON")
	modelsCmd.Flags().BoolVar(&flagModelsCheck, "check", false, "Check conclave's default and cheap models against the catalog; exit 2 if any are missing, 3 if the catalog is unreachable")
	modelsCmd.Flags().BoolVar(&flagModelsAll, "all", false, "Include OpenRouter-only variants (:free, :batch, ...)")
	modelsCmd.Flags().BoolVar(&flagModelsFrontier, "frontier", false, "Show the price-performance Pareto frontier from external quality sources (ADR-018)")
	modelsCmd.Flags().BoolVar(&flagModelsDeciders, "deciders", false, "With --frontier: decision models (Decision Index vs input list price) instead of chat models")
	modelsCmd.Flags().StringVar(&flagModelsAxis, "axis", "", "With --frontier: chat quality axis, intelligence|coding|agentic (default intelligence)")
	modelsCmd.Flags().StringVar(&flagModelsCost, "cost", "", "With --frontier: cost basis, blend|input|task (default blend; --deciders allows only input)")
	modelsCmd.Flags().StringVar(&flagModelsHTML, "html", "", "With --frontier: also write a self-contained HTML report to this file")
	rootCmd.AddCommand(modelsCmd)
}

// validateModelsFlags refuses combinations where one part would be silently
// ignored. --json used to return before --check ran, so a CI step written as
// `models --check --json` exited 0 on drift.
func validateModelsFlags(args []string) error {
	if flagModelsJSON && flagModelsCheck {
		return fmt.Errorf("--json and --check cannot be combined: --check reports drift through its exit code, --json dumps the whole catalog")
	}
	if len(args) == 1 && (flagModelsJSON || flagModelsCheck) {
		return fmt.Errorf("a provider filter (%q) does not apply to --json or --check, which always cover every provider", args[0])
	}
	return nil
}

// === LISTING ===

func runModels(cmd *cobra.Command, args []string) error {
	if flagModelsFrontier || flagModelsDeciders || flagModelsAxis != "" || flagModelsCost != "" || flagModelsHTML != "" {
		return runFrontier(cmd, args)
	}
	if err := validateModelsFlags(args); err != nil {
		return err
	}
	// Decider rows come from the hand table (ADR-016), never the OpenRouter
	// catalog, so a decider filter resolves BEFORE the catalog loads: with
	// CONCLAVE_NO_PRICING or no network it must still print and exit 0.
	// validateModelsFlags already refused a filter with --json/--check.
	if len(args) == 1 {
		p := strings.ToLower(strings.TrimSpace(args[0]))
		if _, err := providers.GetDecider(p); err == nil {
			printDeciderPrices(p)
			return nil
		}
	}
	// Both of these mean "unknown", not "wrong", so they exit distinctly from
	// real drift and must never fail a build. Only the chat-provider rows need
	// the catalog: the unfiltered listing still prints the decider section
	// first, then reports the catalog as unavailable (exit 3).
	listing := !flagModelsJSON && !flagModelsCheck && len(args) == 0
	if pricing.Disabled() {
		if listing {
			printDeciderPrices("")
		}
		return withExitCode(ExitCatalogUnavailable,
			fmt.Errorf("pricing catalog is disabled (CONCLAVE_NO_PRICING is set)"))
	}
	cat, err := pricing.Load(cmd.Context(), pricing.Options{ForceRefresh: flagModelsRefresh})
	if cat == nil {
		if err == nil {
			err = fmt.Errorf("no catalog available")
		}
		if listing {
			printDeciderPrices("")
		}
		return withExitCode(ExitCatalogUnavailable, err)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	defer pricing.WaitBackground(3 * time.Second)

	if flagModelsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(modelsJSON{Catalog: cat, Deciders: pricing.DeciderPrices()})
	}

	if flagModelsCheck {
		// Drift is a real finding and gets its own code, so `make check` and CI
		// can fail on it while still skipping an unreachable catalog.
		return withExitCode(ExitDrift, checkDefaults(cat))
	}

	providersToShow := pricing.Providers()
	showDeciders := true
	if len(args) == 1 {
		p := strings.ToLower(strings.TrimSpace(args[0]))
		showDeciders = false
		if _, ok := pricing.VendorPrefix(p); !ok {
			return fmt.Errorf("unknown provider %q (known: %s)", p, strings.Join(pricing.Providers(), ", "))
		}
		providersToShow = []string{p}
	}

	age := time.Since(cat.FetchedAt).Round(time.Minute)
	staleNote := ""
	if cat.Stale {
		staleNote = " (stale, refreshing in background)"
	}
	fmt.Fprintf(os.Stdout, "OpenRouter catalog: %d models, fetched %s ago%s\n", len(cat.Models), age, staleNote)
	fmt.Fprintf(os.Stdout, "Prices are USD per 1M tokens, API mode only; CLI mode is subscription-billed.\n")

	cfg, _ := config.Load()
	for _, p := range providersToShow {
		models := cat.ByVendor(p)
		if flagModelsAll {
			models = allForVendor(cat, p)
		}
		// A vendor/model token (ADR-010) filters by its vendor; say so in the header.
		label := p
		if strings.Contains(p, "/") {
			label, _ = pricing.VendorPrefix(p)
			label += " (OpenRouter vendor)"
		}
		fmt.Fprintf(os.Stdout, "\n%s\n", strings.ToUpper(label))
		if len(models) == 0 {
			fmt.Fprintf(os.Stdout, "  (no models listed)\n")
			continue
		}
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "  MODEL\tCONTEXT\tIN $/M\tOUT $/M\tRELEASED\tNOTE\n")
		for _, m := range models {
			note := ""
			if cfg != nil {
				if cat.Has(p, cfg.Models[p]) && sameModel(cat, p, cfg.Models[p], m) {
					note = "default"
				} else if cat.Has(p, cfg.CheapModels[p]) && sameModel(cat, p, cfg.CheapModels[p], m) {
					note = "cheap"
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n",
				m.Slug(), fmtContext(m.ContextLength), fmtPrice(m.InputPerM), fmtPrice(m.OutputPerM),
				m.Created.Format("2006-01-02"), note)
		}
		tw.Flush()
	}
	if showDeciders {
		printDeciderPrices("")
	}
	return nil
}

// modelsJSON is the --json document: the catalog's own fields at the top
// level (embedded, so the pre-ADR-016 shape is unchanged) plus the additive
// "deciders" array from the hand-maintained decision-model price table.
type modelsJSON struct {
	*pricing.Catalog
	Deciders []pricing.DeciderPrice `json:"deciders"`
}

// printDeciderPrices lists the decision-model price rows (ADR-016) with their
// as_of dates; only one decider when only is set. These rows are NOT in the
// OpenRouter catalog and `--check` does not gate them, so the date is the
// only staleness signal. A decider with no row prints as unpriced.
func printDeciderPrices(only string) {
	rows := pricing.DeciderPrices()
	fmt.Fprintf(os.Stdout, "\nDECISION MODELS (conclave decide; hand-maintained prices, not checked by --check)\n")
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  DECIDER\tMODEL\tIN $/M\tOUT $/M\tAS OF\n")
	for _, d := range providers.AllDeciders() {
		if only != "" && d.Name() != only {
			continue
		}
		priced := false
		for _, r := range rows {
			if r.Decider == d.Name() {
				priced = true
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", r.Decider, r.Model, fmtPrice(r.InPerM), fmtPrice(r.OutPerM), r.AsOf)
			}
		}
		if !priced {
			fmt.Fprintf(tw, "  %s\t%s\tunpriced\t\t\n", d.Name(), d.DefaultModel())
		}
	}
	tw.Flush()
}

// === FRONTIER ===

// Test seams. frontierAPIKey resolves OPENROUTER_API_KEY the way every
// provider does (env, then .env loaded by Execute, then the OS keyring:
// ADR-008); tests replace it so a developer's real keyring cannot leak into
// a "no key" test. frontierBoardOptions lets tests point the Decision Index
// at httptest (the package has no env override for its upstream and mirror
// URLs).
var (
	frontierAPIKey = func() string {
		return providers.NewKeyRotator(providers.OpenRouterKeyEnv).Next()
	}
	frontierBoardOptions decisionindex.Options
)

// Source names the command adds beside the one BuildDecision sets, so the
// sources footer names every dataset a decision point's price came from.
const (
	catalogSourceName      = "OpenRouter decision-model catalog"
	deciderTableSourceName = "Conclave decider price table"
)

type frontierSpec struct {
	kind  frontier.Kind
	axis  frontier.Axis
	basis frontier.CostBasis
}

// validateFrontierFlags resolves the --frontier family, refusing anything
// that would be silently ignored or that ADR-018 forbids: decision models
// have one axis and one honest cost basis (list price per input token), so
// --axis and any other --cost are errors there, not quiet overrides.
func validateFrontierFlags(args []string) (frontierSpec, error) {
	if !flagModelsFrontier {
		return frontierSpec{}, fmt.Errorf("--deciders, --axis, --cost and --html only apply with --frontier")
	}
	switch {
	case flagModelsCheck:
		return frontierSpec{}, fmt.Errorf("--frontier and --check cannot be combined")
	case flagModelsAll:
		return frontierSpec{}, fmt.Errorf("--all does not apply to --frontier")
	case len(args) == 1:
		return frontierSpec{}, fmt.Errorf("a provider filter (%q) does not apply to --frontier", args[0])
	}
	if flagModelsDeciders {
		if flagModelsAxis != "" {
			return frontierSpec{}, fmt.Errorf("--axis does not apply to --deciders: decision models have one axis, the Decision Index")
		}
		if flagModelsCost != "" && flagModelsCost != "input" {
			return frontierSpec{}, fmt.Errorf("--cost %s does not apply to --deciders: decision models are priced only per input token, because no source publishes a per-task or output price for them (ADR-018)", flagModelsCost)
		}
		return frontierSpec{frontier.KindDecision, frontier.AxisDecision, frontier.CostInputPerM}, nil
	}
	spec := frontierSpec{kind: frontier.KindChat, axis: frontier.AxisIntelligence, basis: frontier.CostBlendPerM}
	switch flagModelsAxis {
	case "", "intelligence":
	case "coding":
		spec.axis = frontier.AxisCoding
	case "agentic":
		spec.axis = frontier.AxisAgentic
	default:
		return frontierSpec{}, fmt.Errorf("unknown --axis %q (intelligence, coding or agentic)", flagModelsAxis)
	}
	switch flagModelsCost {
	case "", "blend":
	case "input":
		spec.basis = frontier.CostInputPerM
	case "task":
		spec.basis = frontier.CostPerTask
	default:
		return frontierSpec{}, fmt.Errorf("unknown --cost %q (blend, input or task)", flagModelsCost)
	}
	return spec, nil
}

// runFrontier loads the sources for one frontier, prints it and optionally
// writes the HTML report. Never prompts (Gotcha 9).
func runFrontier(cmd *cobra.Command, args []string) error {
	spec, err := validateFrontierFlags(args)
	if err != nil {
		return err
	}
	// The flags are valid from here on: a source failure is not a usage
	// error, and a usage dump would bury the one line that says what to set.
	cmd.SilenceUsage = true
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	var res frontier.Result
	if spec.kind == frontier.KindChat {
		res, err = loadChatFrontier(ctx, spec)
	} else {
		res, err = loadDecisionFrontier(ctx)
	}
	if err != nil {
		return err
	}
	res.Points = frontier.Pareto(res.Points)

	// Every degraded source is on res.Sources by now, so this loop is the
	// whole warning channel: stderr, never the exit code (Gotcha 5).
	for _, w := range frontier.Warnings(res) {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	if flagModelsHTML != "" {
		if err := writeFrontierHTML(flagModelsHTML, res); err != nil {
			return err
		}
	}
	if flagModelsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return err
		}
	} else if err := frontier.RenderText(os.Stdout, res); err != nil {
		return err
	}
	if flagModelsHTML != "" {
		path := flagModelsHTML
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		// stdout stays pure JSON under --json, so the path goes to stderr there.
		out := os.Stdout
		if flagModelsJSON {
			out = os.Stderr
		}
		fmt.Fprintf(out, "\nHTML report: %s\n", path)
	}
	return nil
}

// loadChatFrontier: the benchmarks feed is the chat frontier's ONLY source,
// so a missing key or an unloadable feed is "no source at all" and exits 3.
// A stale cache standing in for a failed refresh is a warning.
func loadChatFrontier(ctx context.Context, spec frontierSpec) (frontier.Result, error) {
	key := frontierAPIKey()
	if key == "" {
		return frontier.Result{}, withExitCode(ExitCatalogUnavailable, fmt.Errorf(
			"the chat-model frontier needs %[1]s: OpenRouter's benchmarks feed is keyed. Export %[1]s or run `conclave keyring set %[1]s`. Decision models need no key: conclave models --frontier --deciders",
			providers.OpenRouterKeyEnv))
	}
	feed, err := openrouter.LoadFeed(ctx, openrouter.Options{APIKey: key, Refresh: flagModelsRefresh})
	if feed == nil {
		if err == nil {
			err = fmt.Errorf("no data")
		}
		return frontier.Result{}, withExitCode(ExitCatalogUnavailable, fmt.Errorf("no frontier source could be loaded: %w", err))
	}
	res := frontier.BuildChat(feed, spec.axis, spec.basis)
	if err != nil {
		// A feed alongside an error is a stale cache or an unwritten cache:
		// usable, and said so on every source it fed.
		for i := range res.Sources {
			res.Sources[i].Warning = joinWarning(res.Sources[i].Warning, err.Error())
		}
	}
	return res, nil
}

// loadDecisionFrontier needs no key: the board, the mirror and the decision
// catalog are all public. Board and catalog are independent sources; either
// alone still draws a (partly unscored or unpriced) frontier, and only both
// failing exits 3.
func loadDecisionFrontier(ctx context.Context) (frontier.Result, error) {
	opts := frontierBoardOptions
	opts.Refresh = flagModelsRefresh
	board, boardErr := decisionindex.Load(ctx, opts)
	models, catErr := openrouter.LoadDecisionModels(ctx, openrouter.Options{Refresh: flagModelsRefresh})
	if board == nil && models == nil {
		return frontier.Result{}, withExitCode(ExitCatalogUnavailable,
			fmt.Errorf("no frontier source could be loaded: %v; %v", boardErr, catErr))
	}
	table := pricing.DeciderPrices()
	res := frontier.BuildDecision(board, models, table, frontier.CostInputPerM)
	if board == nil && boardErr != nil && len(res.Sources) > 0 {
		// BuildDecision's error text is generic; the load error says why.
		res.Sources[0].Error = boardErr.Error()
	}

	cat := frontier.Source{Name: catalogSourceName, URL: openrouter.DecisionModelsURL}
	switch {
	case models == nil:
		cat.Error = fmt.Sprint(catErr)
	case catErr != nil:
		cat.Warning = catErr.Error() // stale or unwritten cache, still usable
	}
	res.Sources = append(res.Sources, cat)

	// The hand table prices deciders the catalog does not list (Clef). Its
	// newest as_of is the honest snapshot date; `conclave models <decider>`
	// shows each row's own date and citation.
	asOf := ""
	for _, r := range table {
		if r.AsOf > asOf {
			asOf = r.AsOf
		}
	}
	res.Sources = append(res.Sources, frontier.Source{
		Name:     deciderTableSourceName,
		AsOf:     asOf,
		Citation: "hand-maintained list prices; per-row sources: conclave models <decider>",
	})
	return res, nil
}

func joinWarning(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// writeFrontierHTML writes the report via a temp file and rename, so a
// failed render never leaves a half-written report at the user's path.
func writeFrontierHTML(path string, res frontier.Result) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".frontier-*.html")
	if err != nil {
		return fmt.Errorf("write HTML report: %w", err)
	}
	name := tmp.Name()
	if err := frontier.RenderHTML(tmp, res); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("render HTML report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("write HTML report: %w", err)
	}
	// Some Windows setups refuse to rename over an existing file.
	_ = os.Remove(path)
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("write HTML report: %w", err)
	}
	return nil
}

// === CHECK and helpers ===

// checkDefaults is the drift gate: every compiled default and cheap model must
// resolve in the catalog. Meant for CI and pre-release, hence the exit code.
func checkDefaults(cat *pricing.Catalog) error {
	cfg := config.DefaultConfig()
	type row struct{ provider, kind, model, status, hint string }
	var rows []row
	missing := 0
	check := func(kind string, m map[string]string) {
		names := make([]string, 0, len(m))
		for k := range m {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, p := range names {
			model := m[p]
			if entry, ok := cat.Lookup(p, model); ok {
				rows = append(rows, row{p, kind, model, "ok", fmt.Sprintf("%s $%s/$%s", entry.Slug(), fmtPrice(entry.InputPerM), fmtPrice(entry.OutputPerM))})
				continue
			}
			missing++
			hint := "not in catalog"
			if alts := cat.ByVendor(p); len(alts) > 0 {
				hint = "not in catalog; newest listed: " + alts[0].Slug()
			}
			rows = append(rows, row{p, kind, model, "MISSING", hint})
		}
	}
	check("default", cfg.Models)
	check("cheap", cfg.CheapModels)

	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "PROVIDER\tKIND\tMODEL\tSTATUS\tNOTE\n")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.provider, r.kind, r.model, r.status, r.hint)
	}
	tw.Flush()
	if missing > 0 {
		return fmt.Errorf("%d compiled model id(s) are not in the OpenRouter catalog; verify against the vendor and update internal/config/config.go + docs/MODEL_REGISTRY.md", missing)
	}
	fmt.Fprintf(os.Stdout, "\nAll compiled defaults resolve in the catalog (fetched %s).\n", cat.FetchedAt.Format("2006-01-02"))
	return nil
}

func sameModel(cat *pricing.Catalog, provider, id string, m pricing.Model) bool {
	entry, ok := cat.Lookup(provider, id)
	return ok && entry.ID == m.ID
}

func allForVendor(cat *pricing.Catalog, provider string) []pricing.Model {
	vendor, _ := pricing.VendorPrefix(provider)
	var out []pricing.Model
	for _, m := range cat.Models {
		if m.Vendor() == vendor {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

func fmtContext(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func fmtPrice(v float64) string {
	switch {
	case v == 0:
		return "0"
	case v < 0.1:
		return fmt.Sprintf("%.3f", v)
	case v < 10:
		return fmt.Sprintf("%.2f", v)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
