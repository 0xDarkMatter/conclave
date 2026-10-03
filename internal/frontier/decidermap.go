// The hand-maintained bridge from Decision Index board display names to
// OpenRouter slugs and Conclave decider names (ADR-018). It exists because the
// board keys rows by display name ("Kev 4B"), which no machine feed matches:
// nothing can derive this map, it can only be verified by hand.
package frontier

// boardMapping is one row of the bridge. Either field may be empty: a
// Cloudflare-only model has a Decider but no OpenRouter Slug; a board model
// Conclave does not route has only a Slug.
type boardMapping struct {
	Slug    string // OpenRouter decision-model catalog slug, when listed there
	Decider string // Conclave decider name (internal/pricing table key), when routed
}

// boardMappings is the verified seed. Entries checked 2026-10-03 against
// Decision Index v0.2.1 (multimodalart/jev-decision-index) and OpenRouter's
// decision-model catalog.
//
// GUARD: hand-maintained by necessity — if an entry goes stale (the board
// renames a row, or a slug moves) the model still appears under its board
// name but silently loses its price and its OpenRouter link, because
// BuildDecision prices only through this map. Nothing detects the staleness:
// re-verify these rows whenever decisionindex.Edition moves, and add a
// decidermap_test.go row for each new entry at the same time.
var boardMappings = map[string]boardMapping{
	"Jev":                  {Slug: "typesafe/jev-1.13", Decider: "jev"},
	"Kev 4B":               {Slug: "jaredpalmer/kev-4b"},
	"Tev1-4B-experimental": {Slug: "togethercomputer/tev1-4b-experimental"},
	"Clef":                 {Decider: "clef"},
	"Clef-flash":           {Decider: "clef-flash"},
}

// mapBoard looks up one board display name. ok=false means unmapped: the
// caller must show the model under its board name with no price (ADR-018),
// never guess a similar-looking target.
func mapBoard(name string) (boardMapping, bool) {
	m, ok := boardMappings[name]
	return m, ok
}
