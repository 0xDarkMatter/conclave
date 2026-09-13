package providers

// Per-provider transport selection (ADR-012).
//
// A provider token is "<provider>[@cli|@api]". The suffix picks the transport
// for THAT provider only; a bare token follows the run's global mode (-g / -c).
// This is what lets one panel mix a metered API call (gemini, whose CLI lost
// its free tier) with subscription-billed CLIs (codex on ChatGPT Pro, claude on
// Claude Max) in a single invocation, instead of billing every provider by key
// because one of them needed it.
//
// Contract:
//   - The suffix is parsed in exactly one place (ParseProviderToken) and never
//     reaches a provider's Name(). Progress lines, --json keys, the judge label
//     and the pricing catalog all see the bare name; the transport travels
//     separately (Response.Transport, TransportOf).
//   - Transport strings are the same two words the response cache uses as its
//     key's mode component ("cli" / "api"); internal/cache pins that equality.
//   - Slash tokens (OpenRouter, ADR-010) are API-only: "@cli" on one is an
//     error, "@api" is accepted and makes -g unnecessary for that token.

import (
	"fmt"
	"strings"
)

// Transport names the path a provider's query takes.
type Transport string

const (
	// TransportDefault means "whatever the run's global mode says".
	TransportDefault Transport = ""
	// TransportCLI wraps the provider's installed coding CLI (subscription-billed).
	TransportCLI Transport = "cli"
	// TransportAPI calls the provider's HTTP API directly (metered key).
	TransportAPI Transport = "api"
)

// transportSep separates the provider name from its transport suffix.
const transportSep = "@"

// ParseProviderToken splits "<provider>[@cli|@api]" into the bare provider
// name and the requested transport. A bare token returns TransportDefault.
// The split is on the LAST "@" so a future slug containing one still parses;
// an unrecognised suffix is an error rather than being passed upstream as
// part of the name, where it would produce a baffling "unknown provider".
func ParseProviderToken(token string) (name string, transport Transport, err error) {
	token = strings.TrimSpace(token)
	i := strings.LastIndex(token, transportSep)
	if i < 0 {
		return token, TransportDefault, nil
	}
	name, suffix := token[:i], token[i+len(transportSep):]
	switch suffix {
	case string(TransportCLI):
		transport = TransportCLI
	case string(TransportAPI):
		transport = TransportAPI
	default:
		return "", TransportDefault, fmt.Errorf("provider token %q: unknown transport suffix %q (want @cli or @api)", token, transportSep+suffix)
	}
	if name == "" {
		return "", TransportDefault, fmt.Errorf("provider token %q: missing provider name before %s%s", token, transportSep, suffix)
	}
	if transport == TransportCLI && strings.Contains(name, "/") {
		return "", TransportDefault, fmt.Errorf("provider token %q: %q is an OpenRouter model (vendor/model) and OpenRouter has no CLI; it is API-only (ADR-010), so drop @cli or use %s@api", token, name, name)
	}
	return name, transport, nil
}

// BareName strips any transport suffix from a token, tolerating malformed
// input (the raw token comes back). For display paths that must never fail on
// what a stricter parse elsewhere will already have rejected.
func BareName(token string) string {
	name, _, err := ParseProviderToken(token)
	if err != nil {
		return token
	}
	return name
}

// transporter is implemented by the registry's override wrapper so the
// transport a provider was resolved with can be recovered from any point in a
// decorator chain (the cache wrapper sits on top of it).
type transporter interface{ Transport() Transport }

// TransportOf reports which transport p was resolved with, following the
// Unwrap chain the same way preflight does. Providers built outside the
// registry (tests, batch injection) carry no transport and return
// TransportDefault; callers treat that as "unknown", never as a guess.
func TransportOf(p Provider) Transport {
	for depth := 0; p != nil && depth < 8; depth++ {
		// A decorator that declares no transport of its own (TransportDefault)
		// is transparent: keep descending to the one that resolved it.
		if t, ok := p.(transporter); ok && t.Transport() != TransportDefault {
			return t.Transport()
		}
		u, ok := p.(unwrapper)
		if !ok {
			break
		}
		p = u.Unwrap()
	}
	return TransportDefault
}
