// Package jsonscan finds a JSON object inside text that is not only JSON.
//
// Contract: this is the ONE implementation of "locate the object in the prose"
// in the repo. CLIs that promise JSON still print diagnostics around it
// (AGENTS.md Gotcha 13: claude, gemini), and judges wrap their verdict in prose
// and code fences, so every reader of model or CLI output goes through
// FindObject instead of unmarshalling the whole buffer. Keeping one scanner
// means an edge case fixed here (CRLF, escapes, trailing noise) is fixed for
// every caller; the judge used to carry its own hand-rolled brace matcher,
// which scanned quadratically on unbalanced braces.
package jsonscan

import (
	"encoding/json"
	"strings"
)

// FindObject scans s for JSON objects and returns the raw bytes of the first
// one that accept approves. accept sees both the raw object and its top-level
// fields, so it can reject on shape (missing key) or on a failed typed decode
// and let the scan continue.
//
// Every '{' is tried as a candidate start, so noise before, after, or between
// objects is skipped, as is an unbalanced brace inside a log line. A candidate
// is only handed to accept if it decodes as a complete object; nested objects
// are reached only after their parent was rejected, since the parent's '{'
// comes first. json.Decoder rather than Unmarshal so bytes after the object
// (another noise line, CRLF) do not fail the decode; InputOffset bounds the
// returned slice to the object itself.
//
// Cost is linear in the common case (the first '{' is the object wanted). With
// many braces and no acceptable object it is one failed decode per brace, each
// stopping at its first bad token (a "{{" run fails on the second byte).
func FindObject(s string, accept func(raw json.RawMessage, fields map[string]json.RawMessage) bool) (json.RawMessage, bool) {
	for i := 0; i < len(s); i++ {
		start := strings.IndexByte(s[i:], '{')
		if start < 0 {
			break
		}
		i += start

		dec := json.NewDecoder(strings.NewReader(s[i:]))
		var fields map[string]json.RawMessage
		if err := dec.Decode(&fields); err != nil {
			continue
		}
		raw := json.RawMessage(s[i : i+int(dec.InputOffset())])
		if !accept(raw, fields) {
			continue
		}
		return raw, true
	}
	return nil, false
}
