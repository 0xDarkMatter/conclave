// Slash-routed decision models (ADR-017): any `vendor/model` token given to
// `conclave decide` is a decider served by OpenRouter's Decisions API,
// POST https://openrouter.ai/api/alpha/decisions, with OPENROUTER_API_KEY.
// It is the ADR-010 pattern applied to the ADR-016 class: the token is both
// the decider name and the model id, built on demand, never listed in
// AllDeciders(), and never a chat Provider.
//
// Every OpenRouter decision model speaks the bare System One shape (no
// Workers AI envelope) and reports usage.cost, which prices the call instead
// of the hand table (probed live against six vendors, 2026-10-03).
package providers

import "os"

const openRouterDecisionsURL = "https://openrouter.ai/api/alpha/decisions"

// NewOpenRouterDecider builds the decider for one slash token. The token keeps
// any OpenRouter variant suffix (":free"), because that suffix is part of the
// model id OpenRouter bills against. CONCLAVE_OPENROUTER_DECISIONS_URL replaces
// the full endpoint, for tests.
func NewOpenRouterDecider(token string) *systemOneDecider {
	endpoint := os.Getenv("CONCLAVE_OPENROUTER_DECISIONS_URL")
	if endpoint == "" {
		endpoint = openRouterDecisionsURL
	}
	return &systemOneDecider{
		apiBaseProvider: apiBaseProvider{
			name:         token,
			defaultModel: token,
			apiKeyEnv:    "OPENROUTER_API_KEY",
			baseURL:      endpoint,
		},
		keyRotator: NewKeyRotator("OPENROUTER_API_KEY"),
		backend:    systemOneJev,
	}
}
