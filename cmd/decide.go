package cmd

// `conclave decide` — run a panel of decision models (Jev, Clef, Clef-flash)
// over one state and a typed question set, and report each decider's answers
// plus their equal-weight consensus. Phase 1 of docs/PLAN-decision-models.md.
//
// Contract (ADR-016 unless noted):
//   - Deciders resolve ONLY through providers.GetDecider / AllDeciders and run
//     ONLY from here; a chat provider token is refused with a message naming
//     the deciders. API-only: a transport suffix is an error (GetDecider).
//   - Questions are validated with providers.ValidateDecisionRequest BEFORE
//     any network call or cache lookup, so a malformed set spends nothing.
//   - Never prompts: this command never calls RunInitIfNeeded (AGENTS Gotcha
//     9). No deciders configured is an exit-1 error naming the env vars.
//   - State comes from the positional argument, stdin and -f files, assembled
//     by internal/context exactly as the chat path does (Gotcha 8: nothing
//     sensitive ever has to sit on the command line).
//   - Cache (ADR-011) is opt-in; key = cache.Key("api", decider, model, state,
//     decide.CanonicalQuestions(qs)). A hit is status "success" + cached:true
//     and costs exactly 0.
//   - Exit (cmd/exit.go, cmd/interrupt.go): 0 when >=1 decider succeeded, 1
//     when all failed, 130 on Ctrl-C. The --json envelope prints in every case.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/cache"
	ctxbuild "github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/decide"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"github.com/spf13/cobra"
)

var (
	flagDecideQuestions  string
	flagDecideAsk        string
	flagDecideFiles      []string
	flagDecideJSON       bool
	flagDecideQuiet      bool
	flagDecideTimeout    int
	flagDecideNoStdin    bool
	flagDecideMaxContext int64
)

// deciderEnv names the credentials each decider needs, for the "not
// configured" errors. It restates what decide_jev.go / decide_clef.go read;
// keep the two in step when a backend's credentials change.
var deciderEnv = map[string][]string{
	"jev":        {"TYPESAFE_API_KEY"},
	"clef":       {"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"},
	"clef-flash": {"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"},
}

var decideCmd = &cobra.Command{
	Use:   "decide [deciders] [state] (--questions FILE | --ask TEXT)",
	Short: "Ask decision models typed questions and report their consensus",
	Long: `Runs decision models (jev, clef, clef-flash) in parallel over one state and
a typed question set, then averages their answer probabilities per question
(equal weight). Decision models are not chat providers: they answer only
noul (0-1), choice and score questions, and they run only from this command.

The state is the positional text plus piped stdin plus each -f file. With a
single positional argument, it is read as the decider list when it is a
comma list of decider (or provider) names, otherwise as the state. Omit the
deciders to use every configured one.

Setup: TYPESAFE_API_KEY (jev); CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID
(clef, clef-flash). Environment, ~/.config/conclave/.env or the OS keyring.

Examples:
  conclave decide clef,jev -f ticket.txt --questions triage.yaml --json
  conclave decide "Server down since 9am, customers locked out" --ask "Is this urgent?"
  cat ticket.txt | conclave decide --questions triage.yaml -q`,
	Args: cobra.MaximumNArgs(2),
	RunE: runDecide,
}

func init() {
	decideCmd.Flags().StringVar(&flagDecideQuestions, "questions", "", "Questions file (.yaml/.yml/.json) in the System One wire shape")
	decideCmd.Flags().StringVar(&flagDecideAsk, "ask", "", `Shorthand: one noul (0-1) question, id "q"`)
	decideCmd.Flags().StringSliceVarP(&flagDecideFiles, "file", "f", nil, "Include file content in the state (repeatable)")
	decideCmd.Flags().BoolVar(&flagDecideJSON, "json", false, "Output the structured JSON envelope")
	decideCmd.Flags().BoolVarP(&flagDecideQuiet, "quiet", "q", false, "Print only the consensus answers, one id=value per line")
	decideCmd.Flags().IntVarP(&flagDecideTimeout, "timeout", "t", 30, "Per-decider timeout in seconds")
	decideCmd.Flags().BoolVar(&flagDecideNoStdin, "no-stdin", false, "Ignore stdin even if piped")
	decideCmd.Flags().Int64Var(&flagDecideMaxContext, "max-context", 500000, "Max total state size in bytes")
	// GUARD: --cache/--no-cache bind the ROOT command's variables on purpose,
	// so resolveCache (root.go) serves both commands and the two cannot drift
	// on TTL parsing or CONCLAVE_CACHE_TTL precedence. Safe only because both
	// registrations share the same zero defaults; init order then cannot
	// matter. Giving decide its own vars means copying resolveCache.
	decideCmd.Flags().StringVar(&flagCache, "cache", "", "Reuse identical decisions for this TTL, e.g. --cache or --cache=6h (default 24h; also CONCLAVE_CACHE_TTL=<hours>)")
	decideCmd.Flags().Lookup("cache").NoOptDefVal = "24h"
	decideCmd.Flags().BoolVar(&flagNoCache, "no-cache", false, "Never read or write the response cache, overriding CONCLAVE_CACHE_TTL")
	rootCmd.AddCommand(decideCmd)
}

func runDecide(cmd *cobra.Command, args []string) error {
	if (flagDecideQuestions == "") == (flagDecideAsk == "") {
		return fmt.Errorf("give exactly one of --questions FILE or --ask TEXT")
	}
	if flagDecideJSON && flagDecideQuiet {
		return fmt.Errorf("--json and -q are mutually exclusive")
	}
	if flagDecideTimeout <= 0 {
		return fmt.Errorf("--timeout must be a positive number of seconds")
	}
	// Past usage checks, failures are runtime ones; see runConclave.
	cmd.SilenceUsage = true

	deciderArg, stateArg := splitDecideArgs(args)

	var qs map[string]providers.Question
	if flagDecideAsk != "" {
		qs = decide.AskQuestion(flagDecideAsk)
	} else {
		var err error
		if qs, err = decide.LoadQuestions(flagDecideQuestions); err != nil {
			return err
		}
	}

	deciders, err := resolveDeciders(deciderArg)
	if err != nil {
		return err
	}

	built, err := ctxbuild.Build(ctxbuild.Options{
		Files:       flagDecideFiles,
		MaxSize:     flagDecideMaxContext,
		IgnoreStdin: flagDecideNoStdin,
	})
	if err != nil {
		return fmt.Errorf("context error: %w", err)
	}
	// Same join as the chat path's full prompt (runConclave): context first,
	// then the positional text.
	state := stateArg
	if built.Content != "" {
		state = built.Content
		if stateArg != "" {
			state += "\n\n" + stateArg
		}
	}
	if strings.TrimSpace(state) == "" {
		return fmt.Errorf("no state to decide on: pass it as an argument, with -f, or on stdin")
	}

	req := providers.DecisionRequest{State: state, Questions: qs}
	if err := providers.ValidateDecisionRequest(req); err != nil {
		return fmt.Errorf("invalid questions (nothing was sent): %w", err)
	}

	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	responseCache := resolveCache()
	start := time.Now()
	results := runDeciders(parent, deciders, req, responseCache)
	env := decide.BuildEnvelope(qs, results, time.Since(start))

	out := cmd.OutOrStdout()
	switch {
	case flagDecideJSON:
		err = decide.RenderJSON(out, env)
	case flagDecideQuiet:
		err = decide.RenderQuiet(out, env)
	default:
		err = decide.RenderHuman(out, env, qs)
	}
	if err != nil {
		return err
	}

	if parent.Err() != nil {
		return errDecideInterrupted
	}
	if env.Meta.Succeeded == 0 {
		return fmt.Errorf("all %d decider(s) failed", env.Meta.Requested)
	}
	return nil
}

var errDecideInterrupted = errors.New("interrupted: decisions above are partial")

// splitDecideArgs separates the optional decider list from the state. Two
// args: deciders then state. One arg is the decider list only when it looks
// like one (see looksLikeDeciderList); otherwise it is the state.
func splitDecideArgs(args []string) (deciders, state string) {
	switch len(args) {
	case 2:
		return args[0], args[1]
	case 1:
		if looksLikeDeciderList(args[0]) {
			return args[0], ""
		}
		return "", args[0]
	}
	return "", ""
}

// looksLikeDeciderList is true for a whitespace-free comma list whose every
// token is a decider name, a chat provider name, or carries "@" or "/". The
// provider cases are deliberate: `conclave decide gemini ...` must reach
// resolveDeciders and be refused with the decider list, not be silently
// decided on as the one-word state "gemini".
func looksLikeDeciderList(arg string) bool {
	if arg == "" || strings.ContainsAny(arg, " \t\r\n") {
		return false
	}
	known := map[string]bool{}
	for _, d := range providers.AllDeciders() {
		known[d.Name()] = true
	}
	for _, p := range pricing.Providers() {
		known[p] = true
	}
	for _, tok := range strings.Split(arg, ",") {
		tok = strings.TrimSpace(tok)
		if !known[tok] && !strings.ContainsAny(tok, "@/") {
			return false
		}
	}
	return true
}

// resolveDeciders turns the comma list into deciders, or picks every
// available one when the list is empty. Every failure here happens before
// any spend.
func resolveDeciders(list string) ([]providers.Decider, error) {
	if strings.TrimSpace(list) == "" {
		var out []providers.Decider
		for _, d := range providers.AllDeciders() {
			if d.IsAvailable() {
				out = append(out, d)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no decision models configured: set TYPESAFE_API_KEY (jev), or CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID (clef, clef-flash)")
		}
		return out, nil
	}

	var out []providers.Decider
	seen := map[string]bool{}
	for _, tok := range strings.Split(list, ",") {
		if tok = strings.TrimSpace(tok); tok == "" {
			continue
		}
		d, err := providers.GetDecider(tok)
		if err != nil {
			if _, ok := pricing.VendorPrefix(providers.BareName(tok)); ok {
				return nil, fmt.Errorf("%s is a chat provider, not a decision model; `conclave decide` takes jev, clef or clef-flash (ask chat providers with `conclave %s \"...\"`)", tok, tok)
			}
			return nil, err
		}
		if seen[d.Name()] {
			continue
		}
		seen[d.Name()] = true
		if !d.IsAvailable() {
			return nil, fmt.Errorf("decision model %s is not configured: set %s", d.Name(), strings.Join(deciderEnv[d.Name()], " and "))
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no deciders given")
	}
	return out, nil
}

// runDeciders queries every decider in parallel, each under its own -t
// timeout, consulting the response cache first when one is enabled.
func runDeciders(parent context.Context, deciders []providers.Decider, req providers.DecisionRequest, rc *cache.Cache) map[string]decide.DeciderResult {
	// The question set is the second free-text input of a decision; it takes
	// cache.Key's system slot (ADR-016 on ADR-011). Built once: it is the same
	// canonical string for every decider in this run.
	canonical := decide.CanonicalQuestions(req.Questions)
	state, _ := req.State.(string)

	results := make(map[string]decide.DeciderResult, len(deciders))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, d := range deciders {
		wg.Add(1)
		go func(d providers.Decider) {
			defer wg.Done()
			r := runOneDecider(parent, d, req, rc, cache.Key("api", d.Name(), d.DefaultModel(), state, canonical))
			mu.Lock()
			results[d.Name()] = r
			mu.Unlock()
		}(d)
	}
	wg.Wait()
	return results
}

func runOneDecider(parent context.Context, d providers.Decider, req providers.DecisionRequest, rc *cache.Cache, key string) decide.DeciderResult {
	model := d.DefaultModel()
	if rc != nil {
		if e, ok := rc.Get(key); ok {
			var dec providers.Decision
			if err := json.Unmarshal([]byte(e.Response), &dec); err == nil {
				m := &providers.Metrics{Cached: true}
				if e.Metrics != nil {
					m.InputTokens, m.OutputTokens = e.Metrics.InputTokens, e.Metrics.OutputTokens
				}
				// A hit charges nothing: Priced with CostUSD 0, never the
				// stored call's cost (ADR-011).
				return decide.DeciderResult{
					Model: orDefault(dec.Model, e.Model), Status: decide.StatusSuccess,
					Answers: dec.Answers, Metrics: m, Cached: true, Priced: true,
				}
			}
			// An entry that does not decode is a miss, like every cache fault.
		}
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(flagDecideTimeout)*time.Second)
	defer cancel()
	dec, dur, metrics, err := d.Decide(ctx, req, model)
	if err != nil {
		return decide.DeciderResult{Status: decide.StatusError, DurationMs: dur.Milliseconds(), Error: err.Error()}
	}

	r := decide.DeciderResult{
		Model: orDefault(dec.Model, model), Status: decide.StatusSuccess,
		DurationMs: dur.Milliseconds(), Answers: dec.Answers, Metrics: metrics,
	}
	if metrics != nil {
		// Priced from the hand table only (ADR-016); unpriced leaves CostUSD
		// unset and Priced false, never an error.
		if cost, ok := pricing.DeciderCost(d.Name(), r.Model, metrics.InputTokens, metrics.OutputTokens); ok {
			metrics.CostUSD = cost
			r.Priced = true
		}
	}
	if rc != nil {
		if b, err := json.Marshal(dec); err == nil {
			if err := rc.Put(&cache.Entry{Key: key, Mode: "api", Provider: d.Name(), Model: model, Response: string(b), Metrics: metrics}); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: could not cache %s decision: %v\n", d.Name(), err)
			}
		}
	}
	return r
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
