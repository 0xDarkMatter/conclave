// Tests for the hand-maintained board-name bridge. The map is verified
// against live feeds by hand (2026-10-03); these tests pin exactly what was
// verified so a careless edit cannot silently unprice a model.
package frontier

import "testing"

// TestDeciderMapSeedEntries pins every seed row verbatim: board display name
// -> OpenRouter slug and/or Conclave decider name, as probed 2026-10-03.
func TestDeciderMapSeedEntries(t *testing.T) {
	want := map[string]boardMapping{
		"Jev":                  {Slug: "typesafe/jev-1.13", Decider: "jev"},
		"Kev 4B":               {Slug: "jaredpalmer/kev-4b"},
		"Tev1-4B-experimental": {Slug: "togethercomputer/tev1-4b-experimental"},
		"Clef":                 {Decider: "clef"},
		"Clef-flash":           {Decider: "clef-flash"},
	}
	if len(boardMappings) != len(want) {
		t.Fatalf("map has %d entries; want exactly the %d verified seed rows (add a test row when you verify a new one)",
			len(boardMappings), len(want))
	}
	for name, m := range want {
		got, ok := mapBoard(name)
		if !ok {
			t.Errorf("%q missing from the map; the model would show up unpriced", name)
			continue
		}
		if got != m {
			t.Errorf("mapBoard(%q) = %+v; want %+v", name, got, m)
		}
	}
}

// TestDeciderMapUnknownNameIsNotMapped pins that an unknown board name maps to
// nothing — an unmapped model is displayed under its board name unpriced, and
// must never guess into a similar-looking slug or decider.
func TestDeciderMapUnknownNameIsNotMapped(t *testing.T) {
	for _, name := range []string{"", "Kev", "jev", "CLEF", "D1"} {
		if m, ok := mapBoard(name); ok {
			t.Errorf("mapBoard(%q) = %+v; unknown or case-different names must not map", name, m)
		}
	}
}
