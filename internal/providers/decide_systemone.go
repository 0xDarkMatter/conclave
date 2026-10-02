// Shared System One wire client for the API-only decider class established by
// ADR-016. It owns typed question JSON, local request validation, the common
// Jev-compatible request/response format, and dispatch through ADR-004's sole
// retry loop. Backend files own only endpoint and credential construction.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

type systemOneBackend int

const (
	systemOneJev systemOneBackend = iota
	systemOneClef
)

var questionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// systemOneDecider keeps the common transport path identical across vendors.
// Clef additionally carries an account rotator because account ids use the
// same env/.env/keyring resolution contract as secrets under ADR-008.
type systemOneDecider struct {
	apiBaseProvider
	keyRotator     *KeyRotator
	accountRotator *KeyRotator
	backend        systemOneBackend
}

type systemOneRequest struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type systemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// MarshalJSON builds the polymorphic criteria wire field where its shape is
// part of the protocol: object for choice, array for score, absent for noul.
func (q Question) MarshalJSON() ([]byte, error) {
	type wireQuestion struct {
		Type         string          `json:"type"`
		Instructions string          `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria,omitempty"`
	}
	wire := wireQuestion{Type: q.Type, Instructions: q.Instructions}
	var err error
	switch q.Type {
	case QuestionChoice:
		wire.Criteria, err = json.Marshal(q.Choices)
	case QuestionScore:
		wire.Criteria, err = json.Marshal(q.Scale)
	}
	if err != nil {
		return nil, fmt.Errorf("marshal %s criteria: %w", q.Type, err)
	}
	return json.Marshal(wire)
}

// UnmarshalJSON restores criteria into the field selected by its JSON shape.
// Shape-first decoding preserves invalid noul/unknown criteria for validation
// instead of silently discarding input that would otherwise reach a vendor.
func (q *Question) UnmarshalJSON(data []byte) error {
	type wireQuestion struct {
		Type         string          `json:"type"`
		Instructions string          `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	var wire wireQuestion
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	q.Type = wire.Type
	q.Instructions = wire.Instructions
	q.Choices = nil
	q.Scale = nil
	criteria := bytes.TrimSpace(wire.Criteria)
	if len(criteria) == 0 || bytes.Equal(criteria, []byte("null")) {
		return nil
	}

	switch criteria[0] {
	case '{':
		if err := json.Unmarshal(criteria, &q.Choices); err != nil {
			return fmt.Errorf("decode choice criteria: %w", err)
		}
	case '[':
		if err := json.Unmarshal(criteria, &q.Scale); err != nil {
			return fmt.Errorf("decode score criteria: %w", err)
		}
	default:
		return fmt.Errorf("question criteria must be an object or array")
	}
	if q.Type == QuestionChoice && q.Choices == nil {
		return fmt.Errorf("choice question criteria must be an object")
	}
	if q.Type == QuestionScore && q.Scale == nil {
		return fmt.Errorf("score question criteria must be an array")
	}
	return nil
}

func (p *systemOneDecider) IsAvailable() bool {
	if !p.keyRotator.HasKeys() {
		return false
	}
	return p.backend != systemOneClef || p.accountRotator.HasKeys()
}

// ValidateDecisionRequest rejects malformed questions before the shared HTTP
// client can spend tokens. Sorting makes the first reported map error stable.
func ValidateDecisionRequest(req DecisionRequest) error {
	if len(req.Questions) < 1 || len(req.Questions) > MaxQuestions {
		return fmt.Errorf("questions: got %d; want 1..%d", len(req.Questions), MaxQuestions)
	}
	ids := make([]string, 0, len(req.Questions))
	for id := range req.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		question := req.Questions[id]
		if !questionIDPattern.MatchString(id) {
			return fmt.Errorf("question %q: id must match ^[a-z][a-z0-9_]*$", id)
		}
		if strings.TrimSpace(question.Instructions) == "" {
			return fmt.Errorf("question %q: instructions must not be empty", id)
		}
		switch question.Type {
		case QuestionChoice:
			if len(question.Choices) < 2 {
				return fmt.Errorf("question %q: choice requires at least 2 choices", id)
			}
			if len(question.Scale) != 0 {
				return fmt.Errorf("question %q: choice must not define a score scale", id)
			}
		case QuestionScore:
			if len(question.Scale) < 2 {
				return fmt.Errorf("question %q: score requires at least 2 scale entries", id)
			}
			if len(question.Choices) != 0 {
				return fmt.Errorf("question %q: score must not define choices", id)
			}
		case QuestionNoul:
			if len(question.Choices) != 0 || len(question.Scale) != 0 {
				return fmt.Errorf("question %q: noul must not define criteria", id)
			}
		default:
			return fmt.Errorf("question %q: type %q is invalid; want noul, choice, or score", id, question.Type)
		}
	}
	return nil
}

// Decide sends the shared wire format through apiBaseProvider.doRequest, which
// is deliberately the only retry and BillingError classification path.
func (p *systemOneDecider) Decide(ctx context.Context, req DecisionRequest, model string) (*Decision, time.Duration, *Metrics, error) {
	if err := ValidateDecisionRequest(req); err != nil {
		return nil, 0, nil, err
	}
	if model == "" {
		model = p.defaultModel
	}
	endpoint, err := p.endpoint(model)
	if err != nil {
		return nil, 0, nil, err
	}

	// The model/state/questions names and polymorphic question criteria are the
	// vendor wire contract; do not rename them as internal types evolve.
	body := systemOneRequest{Model: model, State: req.State, Questions: req.Questions}
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", p.keyRotator.Next()),
	}
	start := time.Now()
	responseBody, err := p.doRequest(ctx, http.MethodPost, endpoint, headers, body)
	duration := time.Since(start)
	if err != nil {
		return nil, duration, nil, err
	}

	response, err := decodeSystemOneResponse(responseBody, p.backend == systemOneClef)
	if err != nil {
		return nil, duration, nil, err
	}
	decision := &Decision{Model: response.Model, Answers: response.Answers}
	var metrics *Metrics
	if response.Usage != nil {
		metrics = &Metrics{InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens}
	}
	return decision, duration, metrics, nil
}

func (p *systemOneDecider) endpoint(model string) (string, error) {
	if p.backend == systemOneJev {
		return p.baseURL, nil
	}
	prefix := p.baseURL
	account := p.accountRotator.Next()
	if prefix == "" {
		if account == "" {
			return "", fmt.Errorf("decision model %s not available (CLOUDFLARE_ACCOUNT_ID not set)", p.name)
		}
		prefix = fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run", account)
	}
	modelPath := model
	if !strings.HasPrefix(modelPath, "@cf/") {
		modelPath = "@cf/cloudflare/" + modelPath
	}
	return strings.TrimRight(prefix, "/") + "/" + modelPath, nil
}

func decodeSystemOneResponse(body []byte, allowWorkersAIEnvelope bool) (*systemOneResponse, error) {
	payload := body
	if allowWorkersAIEnvelope {
		var envelope struct {
			Result  json.RawMessage `json:"result"`
			Success *bool           `json:"success"`
		}
		if err := json.Unmarshal(body, &envelope); err == nil && envelope.Success != nil {
			result := bytes.TrimSpace(envelope.Result)
			// Phase 0's 2026-10-02 live probe confirmed only the error wrapper;
			// keep both success shapes until a Workers AI token verifies which one
			// Clef actually returns, then this compatibility branch can be removed.
			if len(result) > 0 && result[0] == '{' {
				payload = result
			}
		}
	}

	var response systemOneResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("parse System One response: %w", err)
	}
	return &response, nil
}
