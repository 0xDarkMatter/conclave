package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/orchestrator"
	"github.com/0xDarkMatter/conclave-cli/internal/output"
	"github.com/0xDarkMatter/conclave-cli/internal/progress"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
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
	flagMaxContext    int64
	flagNoStdin       bool
	flagAll           bool
	flagBlind         bool
	flagGeneral       bool
	flagCheap         bool
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
		// With --all, only need 1 arg (the prompt)
		all, _ := cmd.Flags().GetBool("all")
		if all {
			if len(args) < 1 {
				return fmt.Errorf("requires prompt argument when using --all")
			}
			return nil
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
	rootCmd.Flags().Int64Var(&flagMaxContext, "max-context", 500000, "Max total context size in bytes")
	rootCmd.Flags().BoolVar(&flagNoStdin, "no-stdin", false, "Ignore stdin even if piped")
	rootCmd.Flags().BoolVarP(&flagAll, "all", "a", false, "Use all available providers")
	rootCmd.Flags().BoolVar(&flagBlind, "blind", false, "Anonymize provider names for unbiased judging")
	rootCmd.Flags().BoolVarP(&flagGeneral, "general", "g", false, "General mode: use API providers (no coding restrictions)")
	rootCmd.Flags().BoolVarP(&flagCheap, "cheap", "c", false, "Cheap mode: use smaller/faster models, implies -g (API mode)")

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
		prompt = args[0]
	} else {
		providerNames = strings.Split(args[0], ",")
		prompt = args[1]
	}

	// Apply model overrides from flags
	modelOverrides := parseModelOverrides(flagModel)

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

	// Initialize progress display (quiet if JSON output)
	prog := progress.New(flagJSON || flagQuiet)

	// Build provider info for progress display
	var providerInfos []progress.ProviderInfo
	for _, p := range providerList {
		providerInfos = append(providerInfos, progress.ProviderInfo{
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
	results, err := orch.Run(cmd.Context(), fullPrompt)
	prog.Stop() // Stop spinner before moving on
	if err != nil {
		return err
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
	var providerList []providers.Provider
	var modeLabel, notAvailableMsg string

	if flagGeneral {
		providerList = providers.AllAPIProviders()
		modeLabel = "API (general mode)"
		notAvailableMsg = "no API key"
	} else {
		providerList = providers.AllCLIProviders()
		modeLabel = "CLI (coding mode)"
		notAvailableMsg = "not installed"
	}

	fmt.Printf("Available providers (%s):\n\n", modeLabel)
	for _, p := range providerList {
		status := notAvailableMsg
		if p.IsAvailable() {
			status = "ready"
		}
		fmt.Printf("  %-12s  %-30s  [%s]\n", p.Name(), p.DefaultModel(), status)
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
