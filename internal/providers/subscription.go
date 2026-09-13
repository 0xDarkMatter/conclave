package providers

// Subscription awareness for API mode.
//
// Contract: openai and claude can run on a flat subscription through their
// CLIs (ChatGPT Pro via codex, Claude Max via claude). When a caller runs them
// in API mode (-g / -c / --batch) the metered key is billed instead, which is
// almost always a mistake on a machine where the CLI is logged in: on
// 2026-09-12 every Praxis grade billed OPENAI_API_KEY while the Pro plan sat
// idle. SubscriptionLoggedIn lets cmd/ print one warning at that moment. It is
// advisory: an indeterminate answer is "no", never an error, and it is only
// asked in API mode for the two providers that have a subscription CLI.

import (
	"context"
	"strings"
	"time"
)

// subscriptionProbeTimeout bounds each CLI status call; both answer in well
// under a second when warm and this must never make -g feel slow.
const subscriptionProbeTimeout = 3 * time.Second

// SubscriptionLoggedIn reports whether the provider's CLI holds a live
// subscription login. Only "openai" (codex) and "claude" are subscription
// CLIs; every other name is false.
func SubscriptionLoggedIn(ctx context.Context, provider string) bool {
	ctx, cancel := context.WithTimeout(ctx, subscriptionProbeTimeout)
	defer cancel()
	switch provider {
	case "openai":
		// codex prints its status line to STDERR even on success, hence Combined.
		out, _ := runCommandCombined(ctx, "codex", "login", "status")
		s := strings.ToLower(out)
		return strings.Contains(s, "logged in") && !strings.Contains(s, "not logged in")
	case "claude":
		out, err := runCommand(ctx, "claude", []string{"auth", "status"}, nil)
		if err != nil {
			return false
		}
		// Shared with Preflight: locates the object rather than trusting the
		// whole buffer to be JSON (stray CLI diagnostics land on stdout).
		loggedIn, err := parseClaudeAuthStatus(out)
		return err == nil && loggedIn
	}
	return false
}
