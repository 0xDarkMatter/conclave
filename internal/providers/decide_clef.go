// Cloudflare Clef and Clef-flash registrations for ADR-016's System One
// decider class. Both share credentials and Workers AI endpoint construction;
// separate instances preserve distinct names and default model identifiers.
package providers

import "os"

// NewClefDecider creates the full Clef backend.
func NewClefDecider() *systemOneDecider {
	return newClefDecider("clef", "clef")
}

// NewClefFlashDecider creates the latency-oriented Clef-flash backend.
func NewClefFlashDecider() *systemOneDecider {
	return newClefDecider("clef-flash", "clef-flash")
}

func newClefDecider(name, model string) *systemOneDecider {
	// CONCLAVE_CLEF_BASE_URL is the endpoint prefix through /ai/run; endpoint()
	// appends /@cf/cloudflare/<model>, or an already-qualified @cf/... model.
	// The empty value selects Cloudflare's account-scoped production prefix.
	prefix := os.Getenv("CONCLAVE_CLEF_BASE_URL")
	return &systemOneDecider{
		apiBaseProvider: apiBaseProvider{
			name:         name,
			defaultModel: model,
			apiKeyEnv:    "CLOUDFLARE_API_TOKEN",
			baseURL:      prefix,
		},
		keyRotator:     NewKeyRotator("CLOUDFLARE_API_TOKEN"),
		accountRotator: NewKeyRotator("CLOUDFLARE_ACCOUNT_ID"),
		backend:        systemOneClef,
	}
}
