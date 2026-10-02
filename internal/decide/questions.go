// Package decide loads ADR-016 question files and builds their cache-key form.
// Contract: parsing preserves scalar source text and rejects duplicate ids or
// labels before Go maps can erase them; every question-local error identifies
// the file and question. Request semantics remain owned by provider validation.
// A file contains only the question mapping; state arrives through CLI input.
// CanonicalQuestions is a cache-key input whose grammar is fixed at its builder.
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

// wireQuestion retains the criteria node because decoding into any would
// coerce YAML scalars and collapse distinct source labels before validation.
type wireQuestion struct {
	Type         string
	Instructions string
	Criteria     *yaml.Node
}

// LoadQuestions reads a questions file: YAML (.yaml/.yml) or JSON (.json).
// It returns the question set ready for a DecisionRequest; semantic rules
// (counts, minimum criteria, instructions present) are ValidateDecision
// Request's to enforce, not this loader's.
func LoadQuestions(path string) (map[string]providers.Question, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return decodeQuestionsFile(path, false)
	case ".json":
		return decodeQuestionsFile(path, true)
	default:
		return nil, fmt.Errorf("%s: unsupported questions file extension %q (want .yaml, .yml or .json)", path, filepath.Ext(path))
	}
}

// decodeQuestionsFile parses both syntaxes into yaml.Node, whose mapping
// entries preserve order, duplicates and scalar source spelling. JSON gets an
// encoding/json syntax pass first so YAML's broader grammar cannot leak into a
// .json file; its duplicate members remain visible in the node pass.
func decodeQuestionsFile(path string, jsonFile bool) (map[string]providers.Question, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err // os errors already carry the path
	}
	if jsonFile {
		var syntaxOnly any
		if err := json.Unmarshal(b, &syntaxOnly); err != nil {
			return nil, fmt.Errorf("%s: not a questions file (want a JSON object of question id to question): %w", path, err)
		}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		// Parser-level failures such as excessive aliasing occur before a safe
		// owning question can be identified; the file is the narrowest truthful
		// context for those errors.
		return nil, fmt.Errorf("%s: not a questions file (want a mapping of question id to question): %w", path, err)
	}
	if len(doc.Content) == 0 {
		return map[string]providers.Question{}, nil
	}
	root := resolveAlias(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: not a questions file (want a mapping of question id to question), got a %s", path, nodeKind(root))
	}
	qs := make(map[string]providers.Question, len(root.Content)/2)
	for i := 0; i < len(root.Content); i += 2 {
		id, err := scalarNodeString(root.Content[i])
		if err != nil {
			return nil, fmt.Errorf("%s: question id: %w", path, err)
		}
		if _, exists := qs[id]; exists {
			return nil, fmt.Errorf("%s: question %q: duplicate question id %q", path, id, id)
		}
		w, err := decodeWireQuestion(root.Content[i+1])
		if err != nil {
			return nil, fmt.Errorf("%s: question %q: %w", path, id, err)
		}
		q, err := toQuestion(w)
		if err != nil {
			return nil, fmt.Errorf("%s: question %q: %w", path, id, err)
		}
		qs[id] = q
	}
	return qs, nil
}

// decodeWireQuestion rejects repeated members while the mapping still retains
// them. Unknown fields remain ignored for compatibility with struct decoding.
func decodeWireQuestion(n *yaml.Node) (wireQuestion, error) {
	n = resolveAlias(n)
	if n.Kind != yaml.MappingNode {
		return wireQuestion{}, fmt.Errorf("question must be a mapping, got a %s", nodeKind(n))
	}
	var w wireQuestion
	seen := make(map[string]struct{}, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		field, err := scalarNodeString(n.Content[i])
		if err != nil {
			return w, fmt.Errorf("question field: %w", err)
		}
		if _, exists := seen[field]; exists {
			return w, fmt.Errorf("duplicate question field %q", field)
		}
		seen[field] = struct{}{}
		value := n.Content[i+1]
		switch field {
		case "type":
			w.Type, err = scalarNodeString(value)
		case "instructions":
			w.Instructions, err = scalarNodeString(value)
		case "criteria":
			w.Criteria = value
		}
		if err != nil {
			return w, fmt.Errorf("field %q: %w", field, err)
		}
	}
	return w, nil
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
		// Optional {"true": ..., "false": ...} meanings (Clef schema, probed
		// 2026-10-03). Key names are checked by ValidateDecisionRequest; only
		// the mapping shape is a parse concern here.
		m, err := criteriaMapping(w.Criteria)
		if err != nil {
			return q, fmt.Errorf("noul criteria must be a mapping with true/false keys: %w", err)
		}
		q.NoulCriteria = m
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
func criteriaMapping(n *yaml.Node) (map[string]string, error) {
	if n == nil || isNullNode(n) {
		return nil, nil
	}
	n = resolveAlias(n)
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("got a %s", nodeKind(n))
	}
	out := make(map[string]string, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		key, err := scalarNodeString(n.Content[i])
		if err != nil {
			return nil, fmt.Errorf("label: %w", err)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate criterion label %q", key)
		}
		desc, err := scalarNodeString(n.Content[i+1])
		if err != nil {
			return nil, fmt.Errorf("criterion %q: %w", key, err)
		}
		out[key] = desc
	}
	return out, nil
}

// criteriaSequence converts a decoded criteria node into score anchors,
// preserving order — the index IS the score value on the wire.
func criteriaSequence(n *yaml.Node) ([]string, error) {
	if n == nil || isNullNode(n) {
		return nil, nil
	}
	n = resolveAlias(n)
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("got a %s", nodeKind(n))
	}
	out := make([]string, 0, len(n.Content))
	for i, item := range n.Content {
		s, err := scalarNodeString(item)
		if err != nil {
			return nil, fmt.Errorf("anchor %d: %w", i, err)
		}
		out = append(out, s)
	}
	return out, nil
}

// scalarNodeString returns source spelling rather than a decoded Go value.
// That keeps YAML labels `1`, `1.0` and `yes` distinct and stable in cache keys.
func scalarNodeString(n *yaml.Node) (string, error) {
	n = resolveAlias(n)
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("want plain text, got a nested %s", nodeKind(n))
	}
	if n.Tag == "!!null" {
		return "", nil // `key:` retains the historical empty-description form
	}
	return n.Value, nil
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNullNode(n *yaml.Node) bool {
	n = resolveAlias(n)
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

// nodeKind names syntax nodes without exposing yaml.v3 internals to users.
func nodeKind(n *yaml.Node) string {
	if n == nil {
		return "null"
	}
	switch n.Kind {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "list"
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return "null"
		}
		return "scalar"
	default:
		return "node"
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
	NoulCriteria map[string]string `json:"noul_criteria,omitempty"`
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
//	  - noul criteria render as "noul_criteria" (keys sorted), a field name no
//	    other type uses, so a noul and a choice with the same labels differ;
//	  - criteria is omitted when it parsed as absent (structurally invalid
//	    sets never get this far — LoadQuestions errors).
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
			NoulCriteria: q.NoulCriteria,
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
