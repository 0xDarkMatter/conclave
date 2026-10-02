// Shared System One wire client for the API-only decider class established by
// ADR-016. It owns typed question JSON, local request validation, the common
// Jev-compatible request/response format, and dispatch through ADR-004's sole
// retry loop. Backend files own only endpoint and credential construction.
//
// Invariants beyond ADR-016:
//   - A 2xx body is verified, not trusted: a success:false envelope, an
//     answerless body, a missing question id or a type-mismatched answer is
//     an error, never an empty Decision consensus would count as a success.
//   - Errors returned to callers never contain the Cloudflare account id or
//     the API token; transport errors embed the request URL, so it is redacted.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// modelIDPattern bounds model ids before they are joined into the Clef request
// path: the id is concatenated unescaped, so a "?" or "#" would silently turn
// into the URL's query or fragment instead of a path segment. Documented ids
// and the constructed "@cf/cloudflare/..." prefix all match.
var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9@/._-]+$`)

// accountSegmentPattern finds the path segment after "/accounts/" inside
// rendered error text (the URL appears quoted there), for render-time redaction.
var accountSegmentPattern = regexp.MustCompile(`/accounts/[^/?#\s"']+`)

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
	// Reached only when criteria was present and non-null: ANY criteria on a
	// noul question is invalid, including {} and [], which decode into
	// empty-but-non-nil fields a len() check would treat as absence. This is
	// the wire-side partner of the non-nil check in ValidateDecisionRequest.
	if q.Type == QuestionNoul {
		return fmt.Errorf("noul question must not define criteria")
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

// ValidateDecisionRequest rejects malformed questions and states before the
// shared HTTP client can spend tokens. Sorting makes the first reported map
// error stable.
func ValidateDecisionRequest(req DecisionRequest) error {
	if len(req.Questions) < 1 || len(req.Questions) > MaxQuestions {
		return fmt.Errorf("questions: got %d; want 1..%d", len(req.Questions), MaxQuestions)
	}
	// A nil or blank state fails locally instead of spending a request the
	// vendor must refuse. Only the provably-empty cases are rejected here;
	// other JSON state shapes are the vendor's to judge.
	switch state := req.State.(type) {
	case nil:
		return fmt.Errorf("state: must not be empty")
	case string:
		if strings.TrimSpace(state) == "" {
			return fmt.Errorf("state: must not be empty")
		}
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
			// Labels are the answer vocabulary (probabilities key on them), so
			// a blank label is unusable output, not a style problem.
			for label := range question.Choices {
				if strings.TrimSpace(label) == "" {
					return fmt.Errorf("question %q: choice labels must not be empty", id)
				}
			}
			if len(question.Scale) != 0 {
				return fmt.Errorf("question %q: choice must not define a score scale", id)
			}
		case QuestionScore:
			if len(question.Scale) < 2 {
				return fmt.Errorf("question %q: score requires at least 2 scale entries", id)
			}
			// Scale entries are ordinal answer values; a blank one is an
			// unusable answer, not a style problem.
			for _, entry := range question.Scale {
				if strings.TrimSpace(entry) == "" {
					return fmt.Errorf("question %q: scale entries must not be empty", id)
				}
			}
			if len(question.Choices) != 0 {
				return fmt.Errorf("question %q: score must not define choices", id)
			}
		case QuestionNoul:
			// Non-nil, not merely non-empty: ANY criteria on noul is invalid,
			// including the empty {} / [] the wire decoder materializes, so an
			// empty map must not pass for absence.
			if question.Choices != nil || question.Scale != nil {
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
		// Transport failures embed the full request URL (Go's *url.Error), and
		// Clef's URL carries the account id in its path; redact before the
		// error reaches stderr or the --json "error" field. The token is a
		// header and never appears in these errors.
		return nil, duration, nil, redactAccountID(err)
	}

	response, err := decodeSystemOneResponse(responseBody, p.backend == systemOneClef)
	if err != nil {
		return nil, duration, nil, err
	}
	if err := checkAnswerCoverage(response.Answers, req.Questions); err != nil {
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
	// Checked before any URL construction so an unsafe id can never reach the
	// wire, on either backend: Jev carries the id in the request body where it
	// is harmless, but rejecting here keeps one rule for the whole class.
	if !modelIDPattern.MatchString(model) {
		return "", fmt.Errorf("model id %q is invalid: only [A-Za-z0-9@/._-] is allowed", model)
	}
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
			// A 2xx body can still report failure: Workers AI wraps errors as
			// HTTP 200 + success:false. parseAPIError renders the Cloudflare
			// errors[] shape ("HTTP 200: ... [code: N]"); the status is 200 by
			// definition here because doRequest only returns 2xx bodies.
			if !*envelope.Success {
				return nil, parseAPIError(http.StatusOK, body)
			}
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

// checkAnswerCoverage turns an answer-less or partial 2xx response into an
// error instead of a Decision consensus would count as a decider that
// succeeded: every requested question id must come back with an answer whose
// type matches the question's. Extra answer ids the vendor adds are left
// alone; only the requested contract is enforced. Sorting keeps the named ids
// stable across map iteration order.
func checkAnswerCoverage(answers map[string]Answer, questions map[string]Question) error {
	if len(answers) == 0 {
		return fmt.Errorf("response contains no answers")
	}
	ids := make([]string, 0, len(questions))
	for id := range questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var missing []string
	var mismatched []string
	for _, id := range ids {
		answer, ok := answers[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if answer.Type != questions[id].Type {
			mismatched = append(mismatched, fmt.Sprintf("%s (want %s, got %s)", id, questions[id].Type, answer.Type))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("response is missing answers for questions: %s", strings.Join(missing, ", "))
	}
	if len(mismatched) > 0 {
		return fmt.Errorf("answer types do not match their questions: %s", strings.Join(mismatched, ", "))
	}
	return nil
}

// redactAccountID keeps the Cloudflare account id out of any error Decide
// returns. doRequest copies the request URL into every transport error (Go's
// *url.Error), and Clef's endpoint embeds the account id in its path, so the
// id would otherwise reach stderr and the --json "error" field.
//
// Two layers are needed because fmt.Errorf bakes the wrapped error's text
// into its message at creation time: patching the url.Error's URL field fixes
// future renders of the inner chain (after an Unwrap) but not the
// already-formatted outer layers, so a render-time redacting wrapper covers
// those. Unwrap preserves the chain for errors.Is/As (e.g. BillingError
// classification).
func redactAccountID(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || !strings.Contains(urlErr.URL, "/accounts/") {
		return err
	}
	urlErr.URL = redactAccountInURL(urlErr.URL)
	return &accountRedactedError{err: err}
}

// accountRedactedError re-renders its chain with the path segment after
// "/accounts/" replaced by "<account>". The character class is the URL path
// grammar (a segment ends at "/", "?", "#", whitespace or a quote), so the
// match cannot run past the account segment into surrounding error prose.
type accountRedactedError struct{ err error }

func (e *accountRedactedError) Error() string {
	return accountSegmentPattern.ReplaceAllString(e.err.Error(), "/accounts/<account>")
}

func (e *accountRedactedError) Unwrap() error { return e.err }

// redactAccountInURL replaces the segment following "/accounts/" in the URL
// path; the tail after the next "/" (ai/run/<model>) is preserved. URLs
// without an /accounts/ segment (Jev, self-hosted prefixes) are returned
// unchanged.
func redactAccountInURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL // an unparseable URL has no parsable account segment
	}
	const marker = "/accounts/"
	idx := strings.Index(parsed.Path, marker)
	if idx < 0 {
		return rawURL
	}
	rest := parsed.Path[idx+len(marker):]
	if slash := strings.Index(rest, "/"); slash >= 0 {
		parsed.Path = parsed.Path[:idx+len(marker)] + "<account>" + rest[slash:]
	} else {
		parsed.Path = parsed.Path[:idx+len(marker)] + "<account>"
	}
	return parsed.String()
}
