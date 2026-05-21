package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/batch"
	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/orchestrator"
	"github.com/0xDarkMatter/conclave-cli/internal/output"
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
	flagBatch       string
	flagWorkers     int
	flagOutput      string
	flagResume      bool
	flagNoRateLimit   bool
	flagRetries       int
	flagSkipPreflight bool
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
  conclave --all --general "Explain the trolley problem" --judge claude`,
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
	rootCmd.Flags().StringSliceVarP(&flagModel, "model", "m", nil, "Override model for provider (format: provider:model)")
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

	rootCmd.Version = version
}

func Execute() {
	// Load API keys from ~/.config/conclave/.env before anything else
	_ = config.LoadEnvFile()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runConclave(cmd *cobra.Command, args []string) error {
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

	// Auto-trigger init if no providers configured
	if !providers.AnyAvailable(flagGeneral) {
		if RunInitIfNeeded(flagGeneral) {
			// Re-check after init
			if !providers.AnyAvailable(flagGeneral) {
				return fmt.Errorf("no providers configured - run 'conclave init' to set up API keys")
			}
		}
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// Parse providers and prompt based on --all flag
	var providerNames []string
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
				providerNames = append(providerNames, p.Name())
			}
		}
		if len(providerNames) == 0 {
			if flagGeneral {
				return fmt.Errorf("no providers available (check API keys)")
			}
			return fmt.Errorf("no providers available (check CLI installations)")
		}
		if len(args) > 0 {
			prompt = args[0]
		}
	} else {
		providerNames = strings.Split(args[0], ",")
		if len(args) > 1 {
			prompt = args[1]
		}
	}

	// Apply model overrides from flags
	modelOverrides := parseModelOverrides(flagModel)

	// Handle batch mode
	if flagBatch != "" {
		return runBatchMode(cmd, cfg, providerNames, prompt, modelOverrides)
	}

	// Build context from stdin and files
	ctx, err := context.Build(context.Options{
		Files:      flagFiles,
		MaxSize:    flagMaxContext,
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
	providerList, err := registry.GetProviders(providerNames, modelOverrides)
	if err != nil {
		return err
	}

	// Preflight auth checks
	if !flagSkipPreflight {
		if failures := providers.RunPreflight(cmd.Context(), providerList); len(failures) > 0 {
			printPreflightFailures(failures)
			return fmt.Errorf("preflight auth check failed for %d provider(s)", len(failures))
		}
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
	progressCallback := func(provider string, started bool, duration time.Duration, tokens int, err error) {
		if started {
			prog.ProviderStart(provider)
		} else {
			prog.ProviderDone(provider, duration, tokens, err)
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
		})
		_ = out.Render(output.Result{
			Query:     prompt,
			Context:   ctx,
			Providers: providerNames,
			JudgeName: flagJudge,
			Responses: results,
			Blind:     flagBlind,
			Timeout:   flagTimeout,
		})
		return orchErr
	}

	// Phase 2: Judge synthesis (unless --no-judge or single provider)
	var verdict *judge.Verdict
	if !flagNoJudge && len(providerList) > 1 {
		judgeProvider, err := registry.GetProvider(flagJudge, modelOverrides)
		if err != nil {
			return fmt.Errorf("judge provider error: %w", err)
		}

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
	})

	return out.Render(output.Result{
		Query:     prompt,
		Context:   ctx,
		Providers: providerNames,
		JudgeName: flagJudge,
		Responses: results,
		Verdict:   verdict,
		Blind:     flagBlind,
		Timeout:   flagTimeout,
	})
}

func listProviders() {
	// When -g is explicitly set, show only that mode (preserves scriptable
	// behavior for callers parsing this output).
	if flagGeneral {
		printProviderList("API (general mode)", providers.AllAPIProviders(), "no API key")
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
	printProviderList("API mode (--general / -g)", providers.AllAPIProviders(), "no API key")
	fmt.Println()
	fmt.Println("Use -g to query in API mode; default is CLI mode.")
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

func parseModelOverrides(overrides []string) map[string]string {
	result := make(map[string]string)
	for _, o := range overrides {
		parts := strings.SplitN(o, ":", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
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

func runBatchMode(cmd *cobra.Command, cfg *config.Config, providerNames []string, defaultPrompt string, modelOverrides map[string]string) error {
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
		if failures := providers.RunPreflight(cmd.Context(), tempProviders); len(failures) > 0 {
			printPreflightFailures(failures)
			return fmt.Errorf("preflight auth check failed for %d provider(s)", len(failures))
		}
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
	fmt.Fprintf(os.Stderr, "\nBatch complete: %d items processed (%s)\n", stats.Total, formatDuration(duration))
	fmt.Fprintf(os.Stderr, "  Success: %d (%.1f%%) | Failed: %d (%.1f%%)\n",
		stats.Succeeded, float64(stats.Succeeded)/float64(stats.Total)*100,
		stats.Failed, float64(stats.Failed)/float64(stats.Total)*100)
	fmt.Fprintf(os.Stderr, "  Estimated cost: $%.4f\n", stats.TotalCost)

	return nil
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
