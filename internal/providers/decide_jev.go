// Typesafe Jev registration for the ADR-016 System One decider class. The
// constructor owns Jev's model, credential, endpoint and ROUTE; request
// validation, retries, and wire decoding remain in the shared client.
//
// Two routes reach the same model (ADR-016 addendum, 2026-10-03):
//   - Typesafe direct, POST https://api.typesafe.ai/v1/systemone, TYPESAFE_API_KEY;
//   - OpenRouter's Decisions API, POST https://openrouter.ai/api/alpha/decisions,
//     OPENROUTER_API_KEY (bare "jev-latest" is mapped onto typesafe/ there).
//
// Direct wins when its key resolves; OpenRouter is the fallback, so a machine
// with only an OpenRouter key still has jev. Both speak the bare (unwrapped)
// System One response; OpenRouter adds usage.cost, id and provider fields.
package providers

import "os"

const (
	jevDefaultModel   = "jev-latest"
	jevDefaultURL     = "https://api.typesafe.ai/v1/systemone"
	jevOpenRouterURL  = "https://openrouter.ai/api/alpha/decisions"
	jevOpenRouterKey  = "OPENROUTER_API_KEY"
	jevTypesafeKeyEnv = "TYPESAFE_API_KEY"
)

// NewJevDecider creates the Jev backend on whichever route has a key.
// CONCLAVE_JEV_BASE_URL replaces the complete endpoint URL of the chosen
// route, rather than a host or path prefix, so tests and self-hosted
// Jev-compatible deployments receive the POST exactly there.
func NewJevDecider() *systemOneDecider {
	keyEnv, endpoint := jevTypesafeKeyEnv, jevDefaultURL
	rotator := NewKeyRotator(jevTypesafeKeyEnv)
	if !rotator.HasKeys() {
		if or := NewKeyRotator(jevOpenRouterKey); or.HasKeys() {
			keyEnv, endpoint, rotator = jevOpenRouterKey, jevOpenRouterURL, or
		}
	}
	if override := os.Getenv("CONCLAVE_JEV_BASE_URL"); override != "" {
		endpoint = override
	}
	return &systemOneDecider{
		apiBaseProvider: apiBaseProvider{
			name:         "jev",
			defaultModel: jevDefaultModel,
			apiKeyEnv:    keyEnv,
			baseURL:      endpoint,
		},
		keyRotator: rotator,
		backend:    systemOneJev,
	}
}
