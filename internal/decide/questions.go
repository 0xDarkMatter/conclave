// Package decide holds the decision-model support code that is NOT a provider
// (ADR-016): loading question files in the System One wire shape, the --ask
// shorthand, the canonical JSON that keys the response cache, and — in
// consensus.go, same package — the equal-weight probability averaging over a
// decider panel.
//
// Contract:
//   - Question SEMANTICS (1-64 questions, id syntax, >=2 criteria, non-empty
//     instructions) are validated by providers.ValidateDecisionRequest, never
//     here. This file only parses: a criteria shape that cannot be decoded
//     into the typed providers.Question fails here, naming the file and the
//     question id. The split is not stylistic — a question whose criteria
//     cannot be routed to Choices or Scale has nowhere to live in the typed
//     struct, so dropping it silently is the only alternative to failing here.
//   - A questions FILE is the questions mapping itself (question id ->
//     question), in the wire shape. State does not belong in it; it comes from
//     the positional argument / -f / stdin, exactly as for a chat prompt.
//   - CanonicalQuestions output is a cache-key input, not a wire format; its
//     grammar is documented at the function.
package decide

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"go.yaml.in/yaml/v3"
)

// wireQuestion mirrors one question as it appears in a questions file: the
// System One wire shape (docs/PLAN-decision-models.md), where `criteria` is
// polymorphic — a mapping for choice, a sequence for score, absent for noul.
//
// Decoding deliberately goes through this struct rather than
// providers.Question: Question's criteria fields are typed and tagged `json:"-"`
// on the wire, and its Marshal/UnmarshalJSON are another lane's concern. This
// lane stands alone (packet constraint), so it owns its own decode.
type wireQuestion struct {
	Type         string `json:"type" yaml:"type"`
	Instructions string `json:"instructions" yaml:"instructions"`
	Criteria     any    `json:"criteria" yaml:"criteria"`
}

// LoadQuestions reads a questions file: YAML (.yaml/.yml) or JSON (.json).
// It returns the question set ready for a DecisionRequest; semantic rules
// (counts, minimum criteria, instructions present) are ValidateDecision
// Request's to enforce, not this loader's.
func LoadQuestions(path string) (map[string]providers.Question, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return decodeQuestionsFile(path, yaml.Unmarshal)
	case ".json":
		return decodeQuestionsFile(path, json.Unmarshal)
	default:
		return nil, fmt.Errorf("%s: unsupported questions file extension %q (want .yaml, .yml or .json)", path, filepath.Ext(path))
	}
}

// decodeQuestionsFile is the one reader behind both formats; yaml.Unmarshal
// and json.Unmarshal share the signature, and both decode into any as
// map[string]any / []any / scalars, so the criteria routing in toQuestion is
// shared verbatim.
func decodeQuestionsFile(path string, unmarshal func([]byte, any) error) (map[string]providers.Question, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err // os errors already carry the path
	}
	var raw map[string]wireQuestion
	if err := unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: not a questions file (want a mapping of question id to question): %w", path, err)
	}
	qs := make(map[string]providers.Question, len(raw))
	// Map order is randomised; errors surface the same regardless of order.
	for id, w := range raw {
		q, err := toQuestion(w)
		if err != nil {
			return nil, fmt.Errorf("%s: question %q: %w", path, id, err)
		}
		qs[id] = q
	}
	return qs, nil
}

// toQuestion routes the polymorphic criteria into the typed fields, based on
// the question's declared type. A shape that cannot be routed is an error
// here (see the package contract for why); an ABSENT criteria parses fine —
// "choice with no criteria" is a semantic violation for validation, not a
// parse failure.
func toQuestion(w wireQuestion) (providers.Question, error) {
	q := providers.Question{Type: w.Type, Instructions: w.Instructions}
	switch w.Type {
	case providers.QuestionNoul:
		if w.Criteria != nil {
			// Nowhere to carry it: accepting would silently drop user input.
			return q, fmt.Errorf("noul takes no criteria, got a %s", kindOf(w.Criteria))
		}
		return q, nil
	case providers.QuestionChoice:
		m, err := criteriaMapping(w.Criteria)
		if err != nil {
			return q, fmt.Errorf("choice criteria must be a mapping of label to description: %w", err)
		}
		q.Choices = m
		return q, nil
	case providers.QuestionScore:
		s, err := criteriaSequence(w.Criteria)
		if err != nil {
			return q, fmt.Errorf("score criteria must be an ordered list of anchors (index = score value): %w", err)
		}
		q.Scale = s
		return q, nil
	default:
		// Unknown type also means criteria cannot be routed.
		return q, fmt.Errorf("unknown question type %q (want noul, choice or score)", w.Type)
	}
}

// criteriaMapping converts a decoded criteria node into choice criteria.
// nil (absent) decodes to nil without error — validation owns the minimum.
func criteriaMapping(v any) (map[string]string, error) {
	var raw map[any]any
	switch c := v.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		raw = make(map[any]any, len(c))
		for k, val := range c {
			raw[k] = val
		}
	case map[any]any:
		// yaml.v3 emits map[any]any when any key is not a string (e.g. an
		// unquoted numeric label); JSON always gives map[string]any.
		raw = c
	default:
		return nil, fmt.Errorf("got a %s", kindOf(v))
	}
	out := make(map[string]string, len(raw))
	for k, val := range raw {
		key, err := scalarString(k)
		if err != nil {
			return nil, fmt.Errorf("label: %w", err)
		}
		desc, err := scalarString(val)
		if err != nil {
			return nil, fmt.Errorf("criterion %q: %w", key, err)
		}
		out[key] = desc
	}
	return out, nil
}

// criteriaSequence converts a decoded criteria node into score anchors,
// preserving order — the index IS the score value on the wire.
func criteriaSequence(v any) ([]string, error) {
	switch c := v.(type) {
	case nil:
		return nil, nil
	case []any:
		out := make([]string, 0, len(c))
		for i, item := range c {
			s, err := scalarString(item)
			if err != nil {
				return nil, fmt.Errorf("anchor %d: %w", i, err)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("got a %s", kindOf(v))
	}
}

// scalarString stringifies one criteria scalar. YAML allows unquoted scalars,
// so `2` and `"2"` are the same label; nested containers have no string form
// a vendor could use, so they error instead of rendering Go's "%!v(...)".
func scalarString(v any) (string, error) {
	switch v.(type) {
	case map[string]any, map[any]any, []any:
		return "", fmt.Errorf("want plain text, got a nested %s", kindOf(v))
	case nil:
		return "", nil // `key:` with no value parses as an empty description
	default:
		return fmt.Sprint(v), nil
	}
}

// kindOf names a decoded node for error messages.
func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any, map[any]any:
		return "mapping"
	case []any:
		return "list"
	default:
		return "scalar"
	}
}

// AskQuestion builds the --ask shorthand from the Phase 1 command spec: one
// noul question, fixed id "q". The id is not a user choice — consensus and
// the --json envelope key answers by it.
func AskQuestion(text string) map[string]providers.Question {
	return map[string]providers.Question{
		"q": {Type: providers.QuestionNoul, Instructions: text},
	}
}

// canonicalQuestion is the fixed-field-order half of the canonical form. A
// struct, not a map, so field order cannot drift with a refactor; json.Marshal
// emits struct fields in declaration order and sorts map keys, which is what
// makes the whole form deterministic.
type canonicalQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Choices      map[string]string `json:"choices,omitempty"`
	Scale        []string          `json:"scale,omitempty"`
}

// CanonicalQuestions renders a question set as the deterministic JSON that
// keys the decision cache. The caller feeds it as cache.Key's SYSTEM
// argument — cache.Key("api", decider, model, state, CanonicalQuestions(qs)) —
// reusing the system slot because a decision has no system prompt and the
// question set is its only second free-text input (ADR-016 on ADR-011).
// cache.Key length-prefixes every component, so state and questions cannot
// bleed into each other in the hash input.
//
// Grammar (fixed here, at the construction site):
//
//		{"<qid>":{"type":T,"instructions":I,"choices":{...}},"<qid>":{...,"scale":[...]}}
//
//	  - question ids appear once each, sorted by byte order, so building the
//	    same set through two different map insertion orders yields one key;
//	  - per question, fields are always type, instructions, then criteria;
//	  - choices labels are sorted — a criteria mapping has no order on the
//	    wire, so any order in the key would be a fiction;
//	  - scale anchors keep their file order — index IS the score value, so
//	    order is semantic and MUST shape the key;
//	  - criteria is omitted for noul (and for criteria that parsed as absent;
//	    structurally invalid sets never get this far — LoadQuestions errors).
//
// Changing this grammar silently orphans every cached decision (new string ->
// new sha256 -> miss), which is safe; the unsafe direction is making two
// DIFFERENT sets render alike, which the sorted/fixed-order rules prevent.
func CanonicalQuestions(qs map[string]providers.Question) string {
	if len(qs) == 0 {
		return "{}"
	}
	out := make(map[string]canonicalQuestion, len(qs))
	for id, q := range qs {
		out[id] = canonicalQuestion{
			Type:         q.Type,
			Instructions: q.Instructions,
			Choices:      q.Choices,
			Scale:        q.Scale,
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		// Unreachable in practice (strings and map[string]string only), but a
		// bare `return ""` would alias EVERY question set to one cache key.
		// Keeping the error text in the output keeps distinct failures
		// distinct instead of collapsing them into one wrong key.
		return fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}
	return string(b)
}
