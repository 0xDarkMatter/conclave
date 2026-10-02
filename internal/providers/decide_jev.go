// Typesafe Jev registration for the ADR-016 System One decider class. The
// constructor owns Jev's model, credential, and full-endpoint override while
// request validation, retries, and wire decoding remain in the shared client.
package providers

import "os"

const (
	jevDefaultModel = "jev-latest"
	jevDefaultURL   = "https://api.typesafe.ai/v1/systemone"
)

// NewJevDecider creates the Typesafe backend. CONCLAVE_JEV_BASE_URL replaces
// the complete endpoint URL, rather than a host or path prefix, so tests and
// self-hosted Jev-compatible deployments receive the POST exactly there.
func NewJevDecider() *systemOneDecider {
	endpoint := os.Getenv("CONCLAVE_JEV_BASE_URL")
	if endpoint == "" {
		endpoint = jevDefaultURL
	}
	return &systemOneDecider{
		apiBaseProvider: apiBaseProvider{
			name:         "jev",
			defaultModel: jevDefaultModel,
			apiKeyEnv:    "TYPESAFE_API_KEY",
			baseURL:      endpoint,
		},
		keyRotator: NewKeyRotator("TYPESAFE_API_KEY"),
		backend:    systemOneJev,
	}
}
