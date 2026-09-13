// Package cmd is conclave's CLI surface: flag parsing, the single-query flow,
// batch mode, and the subcommands.
//
// Section map for this file, which is long because one command owns the whole
// flow:
//
//	Flags and command definition   flag vars, rootCmd, init
//	Entry point                    Execute, exit-code handling
//	Single-query flow              runConclave, the default path
//	Provider listing               --list-providers and its helpers
//	Pricing catalog                catalog load and drift warnings
//	Batch mode                     runBatchMode and its summary
//	Response cache resolution      --cache / CONCLAVE_CACHE_TTL
//	Budget and formatting helpers
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/batch"
	"github.com/0xDarkMatter/conclave-cli/internal/cache"
	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/orchestrator"
	"github.com/0xDarkMatter/conclave-cli/internal/output"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"github.com/0xDarkMatter/conclave-cli/internal/tui"
	"github.com/spf13/cobra"
)

var (
	version string

	// Flags
	flagFiles         []string
	flagJudge         string
	flagNoJudge       bool
	flagTimeout       int
	flagModel         []string
	flagJSON          bool
	flagVerbose       bool
	flagBrief         bool
	flagQuiet         bool
	flagListProviders bool
	flagRaw           bool
	flagMaxContext    int64
	flagNoStdin       bool
	flagAll           bool
	flagBlind         bool
	flagGeneral       bool
	flagCheap         bool

	// Batch mode flags
	flagBatch         string
	flagWorkers       int
	flagOutput        string
	flagResume        bool
	flagNoRateLimit   bool
	flagRetries       int
	flagSkipPreflight bool
	flagBudget        float64

	// Response cache (opt-in). flagCache holds the TTL string; cobra's
	// NoOptDefVal makes a bare --cache mean the default TTL.
	flagCache   string
	flagNoCache bool
)

func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

var rootCmd = &cobra.Command{
	Use:   "conclave <providers> <prompt> [flags]",
	Short: "Multi-LLM consensus tool",
	Long: `Conclave queries multiple LLM providers in parallel and synthesizes a verdict.

Examples:
  # Single provider (no judge)
  conclave gemini "What does this code do?"

  # Multiple providers with judge
  conclave gemini,openai,glm "Is this secure?" --judge claude

  # All available providers
  conclave --all "Is this architecture sound?" --judge claude

  # Pipe file content
  cat src/auth.ts | conclave gemini,openai "Review this code" --judge claude

  # Multiple files
  conclave gemini,openai "Compare these" -f impl_a.go -f impl_b.go

  # JSON output
  conclave gemini,openai "Analyze" --judge claude --json

  # General mode (API-based, no coding restrictions)
  conclave -g gemini,openai,claude "Is democracy under threat?" --judge claude
  conclave --all --general "Explain the trolley problem" --judge claude

  # Any OpenRouter model: a vendor/model token routes through OpenRouter (API mode only)
  conclave -g deepseek/deepseek-v4-pro,anthropic/claude-opus-5 "Compare these" --judge openai/gpt-5.6-sol

  # Mixed transports: pin a provider to its CLI (subscription) or API (metered key) per token
  conclave gemini@api,openai@cli,claude@cli "Grade this answer" --no-judge --json`,
	Args: func(cmd *cobra.Command, args []string) error {
		// Allow no args if --list-providers is set
		listProviders, _ := cmd.Flags().GetBool("list-providers")
		if listProviders {
			return nil
		}

		// With --batch and --all, prompt is optional (can be in JSONL)
		batchFile, _ := cmd.Flags().GetString("batch")
		all, _ := cmd.Flags().GetBool("all")
		if batchFile != "" && all {
			return nil // Prompt can be in JSONL or on command line
		}

		// With --all, only need 1 arg (the prompt)
		if all {
			if len(args) < 1 {
				return fmt.Errorf("requires prompt argument when using --all")
			}
			return nil
		}

		// With --batch, need providers and optional prompt
		if batchFile != "" {
			if len(args) < 1 {
				return fmt.Errorf("requires providers argument when using --batch (or use --all)")
			}
			return nil // Prompt optional with --batch
		}

		if len(args) < 2 {
			return fmt.Errorf("requires at least 2 args: <providers> <prompt>")
		}
		return nil
	},
	RunE: runConclave,
}

func init() {
	rootCmd.Flags().StringSliceVarP(&flagFiles, "file", "f", nil, "Include file content (repeatable)")
	rootCmd.Flags().StringVarP(&flagJudge, "judge", "j", "claude", "LLM that synthesizes verdict")
	rootCmd.Flags().BoolVar(&flagNoJudge, "no-judge", false, "Return raw results, skip synthesis")
	rootCmd.Flags().IntVarP(&flagTimeout, "timeout", "t", 60, "Per-provider timeout in seconds")
	rootCmd.Flags().StringSliceVarP(&flagModel, "model", "m", nil, "Override model for provider (format: provider:model; provider@cli:model also accepted, the override applies on either transport)")
	rootCmd.Flags().BoolVar(&flagJSON, "json", false, "Output structured JSON")
	rootCmd.Flags().BoolVar(&flagVerbose, "verbose", false, "Include full provider responses")
	rootCmd.Flags().BoolVar(&flagBrief, "brief", false, "Short verdict only")
	rootCmd.Flags().BoolVarP(&flagQuiet, "quiet", "q", false, "Minimal output (verdict only)")
	rootCmd.Flags().BoolVar(&flagListProviders, "list-providers", false, "List available providers and exit")
	rootCmd.Flags().BoolVar(&flagRaw, "raw", false, "Raw output: only sentinel-separated provider blocks (for piping). Implies --no-judge.")
	rootCmd.Flags().Int64Var(&flagMaxContext, "max-context", 500000, "Max total context size in bytes")
	rootCmd.Flags().BoolVar(&flagNoStdin, "no-stdin", false, "Ignore stdin even if piped")
	rootCmd.Flags().BoolVarP(&flagAll, "all", "a", false, "Use all available providers")
	rootCmd.Flags().BoolVar(&flagBlind, "blind", false, "Anonymize provider names for unbiased judging")
	rootCmd.Flags().BoolVarP(&flagGeneral, "general", "g", false, "General mode: use API providers (no coding restrictions)")
	rootCmd.Flags().BoolVarP(&flagCheap, "cheap", "c", false, "Cheap mode: use smaller/faster models, implies -g (API mode)")

	// Batch mode flags
	rootCmd.Flags().StringVar(&flagBatch, "batch", "", "JSONL input file for batch processing")
	rootCmd.Flags().IntVar(&flagWorkers, "workers", 5, "Number of parallel workers for batch mode")
	rootCmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file for batch mode (default: stdout)")
	rootCmd.Flags().BoolVar(&flagResume, "resume", false, "Resume batch processing from checkpoint")
	rootCmd.Flags().BoolVar(&flagNoRateLimit, "no-rate-limit", false, "Disable rate limiting (for high-tier API accounts)")
	rootCmd.Flags().IntVar(&flagRetries, "retries", 0, "Retry failed batch items N times with exponential backoff (batch mode only; single-call automatically retries 429/5xx)")
	rootCmd.Flags().BoolVar(&flagSkipPreflight, "skip-preflight", false, "Skip auth preflight checks")
	rootCmd.Flags().StringVar(&flagCache, "cache", "", "Reuse identical provider responses for this TTL, e.g. --cache or --cache=6h (default 24h; also CONCLAVE_CACHE_TTL=<hours>)")
	// A bare --cache takes the default TTL. Consequence of NoOptDefVal: an
	// explicit value must use --cache=6h, not --cache 6h.
	rootCmd.Flags().Lookup("cache").NoOptDefVal = "24h"
	rootCmd.Flags().BoolVar(&flagNoCache, "no-cache", false, "Never read or write the response cache, overriding CONCLAVE_CACHE_TTL")
	rootCmd.Flags().Float64Var(&flagBudget, "budget", 0, "Stop dispatching batch items once estimated spend reaches this many USD (batch mode only; also CONCLAVE_BATCH_BUDGET)")

	rootCmd.Version = version
}

// === Entry point ===

func Execute() {
	// Load API keys from ~/.config/conclave/.env before anything else
	_ = config.LoadEnvFile()

	if err := rootCmd.Execute(); err != nil {
		// A command may ask for a specific exit code so callers can branch on
		// the reason; see cmd/exit.go.
		var coded exitCoder
		if errors.As(err, &coded) {
			os.Exit(coded.ExitCode())
		}
		os.Exit(1)
	}
}

// === Single-query flow ===

func runConclave(cmd *cobra.Command, args []string) error {
	// Past argument validation, every error we return is a RUNTIME failure (a
	// provider is down, a budget cap was reached, a batch was interrupted).
	// Cobra's default is to print the whole usage block after any error from
	// here, which buries a summary the user needs to read under sixty lines of
	// flag help. Setting this here rather than on the command keeps usage where
	// it belongs: on a genuine misuse of the CLI, which Args catches earlier.
	cmd.SilenceUsage = true

	// Handle --list-providers
	if flagListProviders {
		listProviders()
		return nil
	}

	// Cheap mode implies API mode (no CLI tools needed for pipelines)
	if flagCheap {
		flagGeneral = true
	}

	// --raw implies --no-judge: synthesis adds nothing to the pipeline output.
	if flagRaw {
		flagNoJudge = true
	}
	// --raw and --json are both machine formats; choosing both is a user error.
	if flagRaw && flagJSON {
		return fmt.Errorf("--raw and --json are mutually exclusive (both produce machine-readable output; pick one)")
	}

	// Batch mode implies cheap mode (unless -m overrides)
	if flagBatch != "" {
		flagCheap = true
		flagGeneral = true
	}

	// Load config first: the availability check below needs to know whether
	// any provider is pinned to the other transport by config (ADR-012).
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// Auto-trigger init if no providers configured. A token pinned with
	// @api or @cli, or a provider pinned in config.yaml's transports map, may
	// be satisfied by the OTHER transport's setup, so in that case both
	// transports count as "configured".
	pinned := (!flagAll && len(args) > 0 && strings.Contains(args[0], "@")) || len(cfg.Transports) > 0
	if !providers.AnyAvailable(flagGeneral) && !(pinned && providers.AnyAvailable(!flagGeneral)) {
		if RunInitIfNeeded(flagGeneral) {
			// Re-check after init
			if !providers.AnyAvailable(flagGeneral) {
				return fmt.Errorf("no providers configured - run 'conclave init' to set up API keys")
			}
		}
	}

	// Parse providers and prompt based on --all flag. providerTokens keeps any
	// @cli/@api suffix for the registry; output surfaces use the BARE names
	// the resolved providers report (see providerNamesOf).
	var providerTokens []string
	var prompt string

	if flagAll {
		// Get all available providers dynamically based on mode
		var allProviders []providers.Provider
		if flagGeneral {
			allProviders = providers.AllAPIProviders()
		} else {
			allProviders = providers.AllCLIProviders()
		}

		// Check for excluded providers (CONCLAVE_EXCLUDE=glm,grok)
		excluded := make(map[string]bool)
		if ex := os.Getenv("CONCLAVE_EXCLUDE"); ex != "" {
			for _, name := range strings.Split(ex, ",") {
				excluded[strings.TrimSpace(name)] = true
			}
		}

		for _, p := range allProviders {
			if p.IsAvailable() && !excluded[p.Name()] {
				providerTokens = append(providerTokens, p.Name())
			}
		}
		if len(providerTokens) == 0 {
			if flagGeneral {
				return fmt.Errorf("no providers available (check API keys; --all never includes OpenRouter vendor/model tokens, name them explicitly)")
			}
			return fmt.Errorf("no providers available (check CLI installations)")
		}
		if len(args) > 0 {
			prompt = args[0]
		}
	} else {
		// Trim so "a, b" works; a stray space in a vendor/model slug would
		// otherwise be sent to OpenRouter verbatim and rejected.
		for _, n := range strings.Split(args[0], ",") {
			if n = strings.TrimSpace(n); n != "" {
				providerTokens = append(providerTokens, n)
			}
		}
		if len(providerTokens) == 0 {
			return fmt.Errorf("no providers given")
		}
		if len(args) > 1 {
			prompt = args[1]
		}
	}

	// Apply model overrides from flags
	modelOverrides := parseModelOverrides(flagModel)

	// Pricing catalog (advisory; nil when offline or disabled). Loaded once
	// here so both the drift warning and batch cost estimates share it. Any
	// background refresh gets a short grace period to finish writing the cache.
	catalog := loadCatalog(cmd)
	defer pricing.WaitBackground(2 * time.Second)
	// Slash-routed OpenRouter tokens render as the catalog label ("DeepSeek:
	// DeepSeek V4") in progress and output; NameOf is nil-safe, so a missing
	// catalog simply leaves the raw slug.
	providers.SetOpenRouterNamer(catalog.NameOf)

	// Response cache (nil = disabled, which is the default).
	responseCache := resolveCache()

	// --budget only gates batch dispatch. Silently ignoring it on a single
	// query would let someone believe they had capped spend when they had not.
	if flagBudget > 0 && flagBatch == "" {
		fmt.Fprintln(os.Stderr, "  warning: --budget applies to --batch runs only; it does not cap this query.")
	}

	// Handle batch mode
	if flagBatch != "" {
		return runBatchMode(cmd, cfg, providerTokens, prompt, modelOverrides, catalog, responseCache)
	}

	// Build context from stdin and files
	ctx, err := context.Build(context.Options{
		Files:       flagFiles,
		MaxSize:     flagMaxContext,
		IgnoreStdin: flagNoStdin,
	})
	if err != nil {
		return fmt.Errorf("context error: %w", err)
	}

	// Full prompt with context
	fullPrompt := prompt
	if ctx.Content != "" {
		fullPrompt = ctx.Content + "\n\n" + prompt
	}

	// Get provider instances
	registry := providers.NewRegistry(cfg, flagGeneral, flagCheap)
	providerList, err := registry.GetProviders(providerTokens, modelOverrides)
	if err != nil {
		return err
	}
	// Bare names for every output surface: a transport suffix must never
	// leak into --json keys, the judge label or execution.providers (ADR-012).
	providerNames := providerNamesOf(providerList)
	judgeName := providers.BareName(flagJudge)

	// Resolve the judge BEFORE the panel runs. A judge that cannot be built
	// (mode, missing key, malformed slug) must fail before any provider is
	// paid for; with OpenRouter judges a typo is one keystroke away.
	var judgeProvider providers.Provider
	if !flagNoJudge && len(providerList) > 1 {
		judgeProvider, err = registry.GetProvider(flagJudge, modelOverrides)
		if err != nil {
			return fmt.Errorf("judge provider error: %w", err)
		}
		// A slash-routed judge is the one place a catalog miss is fatal rather
		// than advisory: the whole panel would be paid for and then synthesis
		// would 400. The catalog can lag (ADR-009), so --skip-preflight sends
		// the slug anyway.
		if !flagSkipPreflight && providers.IsOpenRouterModel(judgeProvider.Name()) && catalog != nil && !catalog.Has(judgeProvider.Name(), judgeProvider.DefaultModel()) {
			return fmt.Errorf("judge %q is not in the OpenRouter catalog (fetched %s); the panel would run and then fail at synthesis. Check `conclave models %s`, or pass --skip-preflight to send it anyway",
				judgeProvider.DefaultModel(), catalog.FetchedAt.Format("2006-01-02"), judgeProvider.Name())
		}
	}
	checked := withJudge(providerList, judgeProvider)

	warnModelDrift(catalog, checked)
	warnSubscriptionIdle(cmd, checked)
	warnConfigTransportPins(cfg, append(append([]string{}, providerTokens...), flagJudge), checked)

	// Preflight auth checks (judge included, deduplicated by name)
	if !flagSkipPreflight {
		if failures := providers.RunPreflight(cmd.Context(), checked); len(failures) > 0 {
			printPreflightFailures(failures)
			return fmt.Errorf("preflight auth check failed for %d provider(s)", len(failures))
		}
	}

	// Wrap for the response cache AFTER preflight: the wrapper deliberately
	// does not forward the Preflighter interface, so wrapping earlier would
	// silently skip every auth check. See internal/cache/provider.go.
	if responseCache != nil {
		providerList = cache.WrapAll(providerList, responseCache, cacheMode())
	}

	// Initialize progress display (quiet if JSON output)
	prog := tui.New(flagJSON || flagQuiet)

	// Build provider info for progress display
	var providerInfos []tui.ProviderInfo
	for _, p := range providerList {
		providerInfos = append(providerInfos, tui.ProviderInfo{
			Name:        p.Name(),
			DisplayName: providers.DisplayName(p.Name(), p.DefaultModel()),
		})
	}
	prog.RegisterProviders(providerInfos)
	prog.Start()

	// Set up progress callback
	progressCallback := func(provider string, started bool, duration time.Duration, tokens int, cached bool, err error) {
		if started {
			prog.ProviderStart(provider)
		} else {
			prog.ProviderDone(provider, duration, tokens, cached, err)
		}
	}

	// Run orchestration with progress
	orch := orchestrator.New(providerList, flagTimeout).WithProgress(progressCallback)
	results, orchErr := orch.Run(cmd.Context(), fullPrompt)
	prog.Stop() // Stop spinner before moving on

	// Even when all providers fail, we still have a results slice with each
	// provider's individual error. Render it so the user sees the full,
	// untruncated diagnostic (HTTP code, OpenAI error param/code, etc.)
	// instead of only the spinner's truncated single-line preview.
	if orchErr != nil {
		out := output.New(output.Options{
			JSON:    flagJSON,
			Verbose: flagVerbose,
			Brief:   flagBrief,
			Quiet:   flagQuiet,
			Raw:     flagRaw,
			Blind:   flagBlind,
			Timeout: flagTimeout,
			Pricing: catalog,
		})
		_ = out.Render(output.Result{
			Query:     prompt,
			Context:   ctx,
			Providers: providerNames,
			JudgeName: judgeName,
			Responses: results,
			Blind:     flagBlind,
			Timeout:   flagTimeout,
		})
		return orchErr
	}

	// Phase 2: Judge synthesis (unless --no-judge or single provider)
	var verdict *judge.Verdict
	if judgeProvider != nil {
		prog.StartSynthesis()
		j := judge.New(judgeProvider)
		verdict, err = j.Synthesize(cmd.Context(), prompt, results, flagTimeout, flagBlind)
		var synthTokens int
		if verdict != nil {
			synthTokens = verdict.JudgeTokens
		}
		prog.StopSynthesis(synthTokens, err)
	}

	prog.Complete()

	// Format output
	out := output.New(output.Options{
		JSON:    flagJSON,
		Verbose: flagVerbose,
		Brief:   flagBrief,
		Quiet:   flagQuiet,
		Raw:     flagRaw,
		Blind:   flagBlind,
		Timeout: flagTimeout,
		// Dollars are decided per response from Response.Transport (ADR-012).
		Pricing: catalog,
	})

	return out.Render(output.Result{
		Query:     prompt,
		Context:   ctx,
		Providers: providerNames,
		JudgeName: judgeName,
		Responses: results,
		Verdict:   verdict,
		Blind:     flagBlind,
		Timeout:   flagTimeout,
	})
}

// === Provider listing ===

func listProviders() {
	// When -g is explicitly set, show only that mode (preserves scriptable
	// behavior for callers parsing this output).
	if flagGeneral {
		printProviderList("API (general mode)", apiProviderListing(), "no API key")
		printOpenRouterNote()
		return
	}

	// Otherwise show both modes side-by-side so users can see which providers
	// are available in each path and what their default models are. The two
	// modes differ in surprising ways (glm CLI-only, grok uses different
	// defaults), so showing both is the helpful default.
	fmt.Println("Available providers:")
	fmt.Println()
	printProviderList("CLI mode (coding-focused)", providers.AllCLIProviders(), "not installed")
	fmt.Println()
	printProviderList("API mode (--general / -g)", apiProviderListing(), "no API key")
	printOpenRouterNote()
	fmt.Println()
	fmt.Println("Use -g to query in API mode; default is CLI mode.")
	fmt.Println("Pin one provider to a transport with <name>@cli or <name>@api, e.g. gemini@api,openai@cli,claude@cli.")
}

// apiProviderListing is AllAPIProviders plus the non-routable "openrouter"
// row. Listing only: --all and AnyAvailable use the registry's own lists so
// OpenRouter models are never auto-included (ADR-010).
func apiProviderListing() []providers.Provider {
	return append(providers.AllAPIProviders(), providers.OpenRouterListing())
}

func printOpenRouterNote() {
	fmt.Println("    openrouter: any model as a vendor/model slug in place of a provider name,")
	fmt.Println("                e.g. -g deepseek/deepseek-v4-pro (API mode only; `conclave models` lists slugs)")
}

func printProviderList(heading string, providerList []providers.Provider, notAvailableMsg string) {
	fmt.Printf("  %s\n", heading)
	fmt.Printf("  %s\n", strings.Repeat("─", len(heading)))
	for _, p := range providerList {
		status := notAvailableMsg
		if p.IsAvailable() {
			status = "ready"
		}
		fmt.Printf("    %-12s  %-30s  [%s]\n", p.Name(), p.DefaultModel(), status)
	}
}

// withJudge appends the judge to the panel for drift/preflight purposes unless
// it is already a panel member: same name AND same transport (ADR-012). A
// claude@cli panel member does not stand in for a claude API judge; they hold
// different credentials, so the judge's preflight must still run. Nil judge
// returns the panel as is.
func withJudge(panel []providers.Provider, judge providers.Provider) []providers.Provider {
	if judge == nil {
		return panel
	}
	for _, p := range panel {
		if p.Name() == judge.Name() && providers.TransportOf(p) == providers.TransportOf(judge) {
			return panel
		}
	}
	out := make([]providers.Provider, 0, len(panel)+1)
	out = append(out, panel...)
	return append(out, judge)
}

// providerNamesOf lists the bare names the resolved providers report, in
// panel order. This is what output surfaces get instead of the raw tokens.
func providerNamesOf(list []providers.Provider) []string {
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name())
	}
	return names
}

// parseModelOverrides maps "provider:model" pairs by BARE provider name. A
// transport suffix on the key ("openai@cli:gpt-5.6-sol") is accepted and
// dropped: the override applies to that provider whichever transport it runs
// on, because the registry keys overrides by name (ADR-012). A malformed
// suffix keeps the raw key so it simply matches nothing rather than aborting.
func parseModelOverrides(overrides []string) map[string]string {
	result := make(map[string]string)
	for _, o := range overrides {
		parts := strings.SplitN(o, ":", 2)
		if len(parts) == 2 {
			result[providers.BareName(parts[0])] = parts[1]
		}
	}
	return result
}

func printPreflightFailures(failures []providers.PreflightResult) {
	fmt.Fprintf(os.Stderr, "\n  Preflight auth check failed:\n\n")
	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "    ✗  %-12s  %s\n", f.Provider, f.Error)
		fmt.Fprintf(os.Stderr, "       %-12s  → %s\n", "", f.Remediation)
	}
	fmt.Fprintf(os.Stderr, "\n  Use --skip-preflight to bypass.\n\n")
}

// loadCatalog fetches the OpenRouter pricing catalog under the pricing package's
// cache rules. Failures are reported once on stderr (never fatal) and only when
// the user would see other diagnostics anyway.
// === Pricing catalog ===

func loadCatalog(cmd *cobra.Command) *pricing.Catalog {
	catalog, err := pricing.Load(cmd.Context(), pricing.Options{})
	if err != nil && !flagQuiet && !flagJSON && !flagRaw {
		fmt.Fprintf(os.Stderr, "  note: %v\n", err)
	}
	return catalog
}

// warnModelDrift prints one stderr line per provider whose configured model id
// is absent from the OpenRouter catalog. The catalog is a proxy for the vendor
// list, so this is advice, not a refusal: the query proceeds unchanged.
//
// Skipped when: no catalog; the model is a CLI alias with no version digits
// ("sonnet", "opus") which OpenRouter cannot know about; or output is a
// machine format where a stray line would be noise.
// warnSubscriptionIdle prints one stderr line per provider that is about to be
// billed by API key while its CLI holds a subscription login (codex on ChatGPT
// Pro, claude on Claude Max). Decided per provider from its actual transport
// (ADR-012), so a claude@cli beside a -g panel is never warned about. -q and
// --raw silence it, --json does not, because the person running a --json
// pipeline is exactly who needs to see that the metered key is being spent.
// Advisory: the query proceeds. The remedy named is the per-provider suffix,
// which fixes the one provider without moving the whole panel off the API.
// warnConfigTransportPins prints one stderr line per provider whose transport
// was decided by config.yaml's transports map rather than by the invocation.
// A config pin is the one input that can move billing between a subscription
// and a metered key without appearing in the command line, which is the exact
// invisibility the @suffix exists to remove, so the run says when it happened.
// Only fires when the pin actually changed the outcome (a bare token that the
// global mode would have sent elsewhere); silent for suffixed tokens and for
// pins that agree with -g/-c. -q and --raw silence it, --json does not.
func warnConfigTransportPins(cfg *config.Config, tokens []string, providerList []providers.Provider) {
	if flagQuiet || flagRaw || cfg == nil || len(cfg.Transports) == 0 {
		return
	}
	suffixed := map[string]bool{}
	for _, tok := range tokens {
		if name, t, err := providers.ParseProviderToken(tok); err == nil && t != providers.TransportDefault {
			suffixed[name] = true
		}
	}
	defaultT := providers.TransportCLI
	if flagGeneral {
		defaultT = providers.TransportAPI
	}
	seen := map[string]bool{}
	for _, p := range providerList {
		name := p.Name()
		if seen[name] || suffixed[name] {
			continue
		}
		seen[name] = true
		pin := cfg.GetTransport(name)
		if pin == "" || providers.Transport(pin) == defaultT || providers.TransportOf(p) != providers.Transport(pin) {
			continue
		}
		fmt.Fprintf(os.Stderr, "  note: %s runs on the %s because config.yaml transports.%s pins it; pass %s@%s to override for this run.\n",
			name, pin, name, name, otherTransport(pin))
	}
}

func otherTransport(t string) string {
	if t == string(providers.TransportCLI) {
		return string(providers.TransportAPI)
	}
	return string(providers.TransportCLI)
}

func warnSubscriptionIdle(cmd *cobra.Command, providerList []providers.Provider) {
	ctx := cmd.Context()
	if flagQuiet || flagRaw {
		return
	}
	seen := map[string]bool{}
	for _, p := range providerList {
		name := p.Name()
		if seen[name] || (name != "openai" && name != "claude") {
			continue
		}
		if providers.TransportOf(p) != providers.TransportAPI {
			continue
		}
		seen[name] = true
		if !providers.SubscriptionLoggedIn(ctx, name) {
			continue
		}
		cli := map[string]string{"openai": "codex", "claude": "claude"}[name]
		fmt.Fprintf(os.Stderr, "  note: %s is running on the API (metered key) while %s is logged in on a subscription; write %s@cli to run it on the plan in this panel, or drop -g.\n", name, cli, name)
	}
}

func warnModelDrift(catalog *pricing.Catalog, providerList []providers.Provider) {
	if catalog == nil || flagQuiet || flagJSON || flagRaw {
		return
	}
	for _, p := range providerList {
		model := p.DefaultModel()
		if !strings.ContainsAny(model, "0123456789") {
			continue
		}
		if _, known := pricing.VendorPrefix(p.Name()); !known || catalog.Has(p.Name(), model) {
			continue
		}
		hint := ""
		if alts := catalog.ByVendor(p.Name()); len(alts) > 0 {
			hint = fmt.Sprintf(" Newest listed: %s.", alts[0].Slug())
		}
		if providers.IsOpenRouterModel(p.Name()) {
			// The token IS the model; naming it twice and suggesting -m is noise.
			fmt.Fprintf(os.Stderr, "  warning: OpenRouter slug %q is not in the catalog (fetched %s); OpenRouter will likely reject it.%s See `conclave models %s`.\n",
				model, catalog.FetchedAt.Format("2006-01-02"), hint, p.Name())
			continue
		}
		fmt.Fprintf(os.Stderr, "  warning: %s model %q is not in the OpenRouter catalog (fetched %s); it may be retired.%s Override with -m %s:<model>.\n",
			p.Name(), model, catalog.FetchedAt.Format("2006-01-02"), hint, p.Name())
	}
}

// === Batch mode ===

func runBatchMode(cmd *cobra.Command, cfg *config.Config, providerNames []string, defaultPrompt string, modelOverrides map[string]string, catalog *pricing.Catalog, responseCache *cache.Cache) error {
	// Open input file
	var input *os.File
	var err error
	if flagBatch == "-" {
		input = os.Stdin
	} else {
		input, err = os.Open(flagBatch)
		if err != nil {
			return fmt.Errorf("failed to open batch file: %w", err)
		}
		defer input.Close()
	}

	// Set up output
	var output *os.File
	if flagOutput == "" || flagOutput == "-" {
		output = os.Stdout
	} else {
		// If resuming, open for append
		if flagResume {
			output, err = os.OpenFile(flagOutput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		} else {
			output, err = os.Create(flagOutput)
		}
		if err != nil {
			return fmt.Errorf("failed to open output file: %w", err)
		}
		defer output.Close()
	}

	// Create registry and preflight auth checks
	registry := providers.NewRegistry(cfg, flagGeneral, flagCheap)
	if !flagSkipPreflight {
		tempProviders, err := registry.GetProviders(providerNames, modelOverrides)
		if err != nil {
			return err
		}
		warnModelDrift(catalog, tempProviders)
		warnSubscriptionIdle(cmd, tempProviders)
		warnConfigTransportPins(cfg, providerNames, tempProviders)
		if failures := providers.RunPreflight(cmd.Context(), tempProviders); len(failures) > 0 {
			printPreflightFailures(failures)
			return fmt.Errorf("preflight auth check failed for %d provider(s)", len(failures))
		}
	}

	budget := resolveBatchBudget()
	// A cap can only bind on spend the tool can actually estimate. Without the
	// catalog, estimates come from the compiled fallback table, which covers
	// only the six built-in providers — an OpenRouter slug would be costed at
	// zero and the cap would never trip. Say so rather than implying a
	// guarantee that is not there.
	if budget > 0 && catalog == nil {
		fmt.Fprintln(os.Stderr, "  warning: --budget is set but the pricing catalog is unavailable; estimates fall back to a compiled table covering only the built-in providers, so the cap may not bind.")
	}

	processor, err := batch.NewProcessor(batch.Options{
		Registry:       registry,
		Config:         cfg,
		ProviderNames:  providerNames,
		ModelOverrides: modelOverrides,
		JudgeName:      flagJudge,
		Workers:        flagWorkers,
		Timeout:        flagTimeout,
		OutputPath:     flagOutput,
		Resume:         flagResume,
		Verbose:        flagVerbose,
		Blind:          flagBlind,
		NoRateLimit:    flagNoRateLimit,
		Retries:        flagRetries,
		Budget:         budget,
		Pricing:        catalog,
		Cache:          responseCache,
	})
	if err != nil {
		return fmt.Errorf("failed to create batch processor: %w", err)
	}
	defer processor.Close() // Clean up checkpoint file handle

	// Run batch processing
	stats, err := processor.Process(cmd.Context(), input, output, defaultPrompt)
	if err != nil {
		return fmt.Errorf("batch processing failed: %w", err)
	}

	// Print summary to stderr
	duration := time.Since(stats.StartTime)
	pct := func(n int) float64 {
		if stats.Completed == 0 {
			return 0
		}
		return float64(n) / float64(stats.Completed) * 100
	}
	fmt.Fprintf(os.Stderr, "\nBatch complete: %d/%d items processed (%s)\n", stats.Completed, stats.Total, formatDuration(duration))
	fmt.Fprintf(os.Stderr, "  Success: %d (%.1f%%) | Failed: %d (%.1f%%)\n",
		stats.Succeeded, pct(stats.Succeeded), stats.Failed, pct(stats.Failed))
	// Shared formatter: a real but sub-tenth-of-a-cent estimate must not print
	// as $0.0000, which reads as free.
	fmt.Fprintf(os.Stderr, "  Estimated cost: %s\n", pricing.FormatUSD(stats.TotalCost))

	// A budget stop is a non-zero exit: the run is incomplete on purpose and a
	// caller in a pipeline must be able to tell that apart from a clean finish.
	// The checkpoint holds every completed id, so --resume continues the rest.
	if stats.BudgetStopped {
		fmt.Fprintf(os.Stderr, "  Budget cap $%.4f reached: %d item(s) not dispatched.\n", stats.Budget, stats.Skipped)
		if flagOutput != "" && flagOutput != "-" {
			fmt.Fprintf(os.Stderr, "  Resume with: conclave ... --batch <input> -o %s --resume\n", flagOutput)
		} else {
			fmt.Fprintln(os.Stderr, "  Use -o <file> --resume to continue a capped run later.")
		}
		return fmt.Errorf("budget cap $%.4f reached after %d item(s); %d not dispatched", stats.Budget, stats.Completed, stats.Skipped)
	}

	// An interrupted run leaves a PARTIAL output file. Exiting 0 here would let
	// a pipeline treat a half-finished JSONL as the complete answer, which is
	// the kind of silent truncation nobody notices until the numbers are wrong.
	if stats.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "  Interrupted: %d item(s) not dispatched; the output is partial.\n", stats.Skipped)
		if flagOutput != "" && flagOutput != "-" {
			fmt.Fprintf(os.Stderr, "  Resume with: conclave ... --batch <input> -o %s --resume\n", flagOutput)
		}
		return fmt.Errorf("batch interrupted after %d of %d item(s); %d not dispatched", stats.Completed, stats.Total, stats.Skipped)
	}

	return nil
}

// === Response cache resolution ===

// cacheMode is the FALLBACK mode component of the response-cache key, used
// only for a provider that does not declare its own transport. Every
// registry-built provider does (ADR-012), and cache.Wrap reads that first, so
// a claude@cli answer is keyed "cli" even under -g. CLI and API paths send
// materially different requests for the same provider name, so their answers
// must never be interchangeable.

func cacheMode() string {
	if flagGeneral {
		return cache.ModeAPI
	}
	return cache.ModeCLI
}

// resolveCache decides whether the opt-in response cache is on for this run:
// --no-cache wins, then --cache[=TTL], then CONCLAVE_CACHE_TTL in hours.
// Returning nil means "no cache", and every call site treats nil as a no-op.
func resolveCache() *cache.Cache {
	if flagNoCache {
		return nil
	}
	if flagCache != "" {
		ttl, err := parseCacheTTL(flagCache)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: ignoring --cache=%q (%v)\n", flagCache, err)
			return nil
		}
		return cache.New("", ttl)
	}
	if raw := strings.TrimSpace(os.Getenv("CONCLAVE_CACHE_TTL")); raw != "" {
		h, err := strconv.ParseFloat(raw, 64)
		if err != nil || h <= 0 {
			fmt.Fprintf(os.Stderr, "  warning: ignoring CONCLAVE_CACHE_TTL=%q (want a positive number of hours)\n", raw)
			return nil
		}
		return cache.New("", time.Duration(h*float64(time.Hour)))
	}
	return nil
}

// parseCacheTTL accepts a Go duration ("6h", "90m") or a bare number of hours.
func parseCacheTTL(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if d, err := time.ParseDuration(v); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("TTL must be positive")
		}
		return d, nil
	}
	h, err := strconv.ParseFloat(v, 64)
	if err != nil || h <= 0 {
		return 0, fmt.Errorf("want a duration like 6h or a positive number of hours")
	}
	return time.Duration(h * float64(time.Hour)), nil
}

// resolveBatchBudget reads the spend cap: --budget wins, then
// CONCLAVE_BATCH_BUDGET, then uncapped. A malformed or non-positive env value
// is ignored with a warning rather than failing the run — a typo in an
// exported variable should not stop a batch that is otherwise free to proceed.
// === Budget and formatting helpers ===

func resolveBatchBudget() float64 {
	if flagBudget > 0 {
		return flagBudget
	}
	raw := strings.TrimSpace(os.Getenv("CONCLAVE_BATCH_BUDGET"))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimPrefix(raw, "$"), 64)
	if err != nil || v <= 0 {
		fmt.Fprintf(os.Stderr, "  warning: ignoring CONCLAVE_BATCH_BUDGET=%q (want a positive number of USD)\n", raw)
		return 0
	}
	return v
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s > 0 {
			return fmt.Sprintf("%dm%ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
