// Decision models ("System One": Typesafe Jev, Cloudflare Clef / Clef-flash) are a
// separate provider class (ADR-016). They take typed questions over a state and
// return typed answers with probabilities, so they do NOT implement Provider:
// a free-text Query would lose the types, `--all` would send them prompts they
// cannot answer, and the response cache / pricing catalog would key them wrong.
//
// Invariants:
//   - Deciders are listed only by AllDeciders(), never AllAPIProviders().
//   - Deciders are API-only and take no @cli/@api transport suffix.
//   - The wire shape below is shared by every backend (Clef is "fully
//     Jev-API compatible"); per-backend differences (URL, auth, response
//     envelope) live in decide_systemone.go, never in these types.
//
// This file is the contract the decide_*.go backends, internal/decide
// (consensus) and cmd/decide.go are all built against. Change it first, alone.
package providers

import (
	"context"
	"time"
)

// Question types, exactly as the wire spells them. "noul" is not a typo:
// it is Typesafe's name for a 0-1 value question.
const (
	QuestionNoul   = "noul"
	QuestionChoice = "choice"
	QuestionScore  = "score"
)

// Wire limits from Clef's published input schema (GET .../ai/models/schema,
// probed live 2026-10-03). Validation enforces them locally so a malformed
// question set fails before any spend.
const (
	MaxQuestions     = 64
	MaxQuestionIDLen = 100
	MaxChoices       = 255
	MaxScaleLevels   = 10
	MaxImages        = 4
)

// Noul criteria keys, as the schema spells them: what a yes (value near 1)
// and a no (value near 0) mean. Both optional.
const (
	NoulCriterionTrue  = "true"
	NoulCriterionFalse = "false"
)

// Question is one typed question. Criteria is polymorphic on the wire: a
// map[label]description for choice, an ordered []string for score (index =
// score value), and an optional {"true": ..., "false": ...} object for noul.
// The typed fields keep that explicit; MarshalJSON in decide_systemone.go
// emits whichever one the type uses.
type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Choices      map[string]string `json:"-"` // choice criteria
	Scale        []string          `json:"-"` // score criteria, ordinal
	NoulCriteria map[string]string `json:"-"` // noul criteria; keys NoulCriterionTrue/False only
}

// DecisionRequest is a backend-neutral request. State is a string or any
// JSON-marshalable structured value (the wire accepts both).
type DecisionRequest struct {
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Answer is one typed answer, decoded from the vendor response. Only the
// field matching Type is meaningful. Confidence is nil when the vendor omits
// it (Jev omits it for noul). Probabilities keys are choice labels for
// choice, and decimal indices ("0", "1", ...) for score.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Decision is one decider's full answer set. Model is the version the vendor
// reports (e.g. "jev-1.13.0"), not the id that was requested.
type Decision struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
}

// Decider is the decision-model counterpart of Provider. Decide returns
// Metrics with InputTokens/OutputTokens from the vendor's usage block; CostUSD
// is left to internal/pricing (deciders.go), never self-priced here.
type Decider interface {
	Name() string
	DefaultModel() string
	IsAvailable() bool
	Decide(ctx context.Context, req DecisionRequest, model string) (*Decision, time.Duration, *Metrics, error)
}
