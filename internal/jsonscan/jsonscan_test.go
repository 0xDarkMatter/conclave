package jsonscan

import (
	"encoding/json"
	"strings"
	"testing"
)

func acceptAll(json.RawMessage, map[string]json.RawMessage) bool { return true }

// TestFindObjectBoundsToObject: the raw slice handed back must be exactly the
// object, not the object plus whatever followed it, or a later Unmarshal of
// that slice fails on the trailing bytes.
func TestFindObjectBoundsToObject(t *testing.T) {
	raw, ok := FindObject("x {\"a\":1} trailing {\"b\":2}", acceptAll)
	if !ok || string(raw) != `{"a":1}` {
		t.Fatalf("got %q, %v", raw, ok)
	}
}

// TestFindObjectLargeNoiseIsFast guards the worst case the scan comment
// promises: many braces and no acceptable object must still finish in
// negligible time (each candidate decode stops at its first bad token).
func TestFindObjectLargeNoiseIsFast(t *testing.T) {
	noise := strings.Repeat("if (x) { y{ } z{{ ", 20000) // ~80k braces, `{ }` decodes but has no key
	if _, ok := FindObject(noise, func(_ json.RawMessage, f map[string]json.RawMessage) bool { _, ok := f["result"]; return ok }); ok {
		t.Fatal("no object expected")
	}
}

// TestFindObjectSkipsARejectedParentToItsChild: rejection must let the scan
// continue into the rejected object, where a nested match can still be found.
func TestFindObjectSkipsARejectedParentToItsChild(t *testing.T) {
	raw, ok := FindObject(`prefix {"outer": {"verdict": "YES"}} suffix`, func(_ json.RawMessage, f map[string]json.RawMessage) bool {
		_, ok := f["verdict"]
		return ok
	})
	if !ok || string(raw) != `{"verdict": "YES"}` {
		t.Fatalf("got %q, %v", raw, ok)
	}
}
