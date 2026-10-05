// Tests for the shared System One wire contract and its Jev and Clef backend
// adaptations. They keep all HTTP traffic inside httptest servers and pin the
// provisional dual Clef success-envelope support required by ADR-016's Phase 0
// finding.
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const systemOneTestResponse = `{"model":"jev-1.13.0","answers":{"department":{"type":"choice","choice":"technical","confidence":0.78,"probabilities":{"technical":0.85,"sales":0.0,"billing":0.15}},"frustration":{"type":"score","score":1.0,"confidence":1.0,"legend":{"0":"Calm","1":"Frustrated","2":"Angry"},"probabilities":{"0":0.0,"1":1.0,"2":0.0}},"is_urgent":{"type":"noul","noul":1.0}},"usage":{"input_tokens":392,"output_tokens":65}}`

func validDecisionRequest() DecisionRequest {
	return DecisionRequest{
		State: "A frustrated customer needs technical help.",
		Questions: map[string]Question{
			"department": {
				Type:         QuestionChoice,
				Instructions: "Route the request.",
				Choices: map[string]string{
					"billing":   "Billing issue",
					"technical": "Technical issue",
				},
			},
		},
	}
}

func TestDecideJevDecodesTypedAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-typesafe-key" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(systemOneTestResponse))
	}))
	defer srv.Close()

	t.Setenv("TYPESAFE_API_KEY", "test-typesafe-key")
	t.Setenv("CONCLAVE_JEV_BASE_URL", srv.URL)
	decision, _, metrics, err := NewJevDecider().Decide(context.Background(), validDecisionRequest(), "")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Model != "jev-1.13.0" {
		t.Errorf("model = %q", decision.Model)
	}
	department := decision.Answers["department"]
	if department.Choice != "technical" || department.Probabilities["technical"] != 0.85 {
		t.Errorf("department = %#v", department)
	}
	frustration := decision.Answers["frustration"]
	if frustration.Score == nil || *frustration.Score != 1 || frustration.Legend["2"] != "Angry" || frustration.Probabilities["1"] != 1 {
		t.Errorf("frustration = %#v", frustration)
	}
	urgent := decision.Answers["is_urgent"]
	if urgent.Noul == nil || *urgent.Noul != 1 {
		t.Errorf("is_urgent = %#v", urgent)
	}
	if metrics == nil || metrics.InputTokens != 392 || metrics.OutputTokens != 65 || metrics.CostUSD != 0 {
		t.Errorf("metrics = %#v", metrics)
	}
}

// Phase 0 (live, 2026-10-03): Clef successes always arrive in the Workers AI
// envelope, so a bare body is a wrong endpoint, not a second valid shape.
func TestClefRequiresWorkersAIEnvelope(t *testing.T) {
	cases := map[string]struct {
		response string
		wantErr  bool
	}{
		"envelope": {`{"result":` + systemOneTestResponse + `,"success":true,"errors":[],"messages":[]}`, false},
		"bare":     {systemOneTestResponse, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/@cf/cloudflare/clef" {
					t.Errorf("path = %q", r.URL.Path)
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			t.Setenv("CLOUDFLARE_API_TOKEN", "test-cf-token")
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
			t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL)
			decision, _, metrics, err := NewClefDecider().Decide(context.Background(), validDecisionRequest(), "")
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "envelope") {
					t.Fatalf("err = %v; want an envelope error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if decision.Model != "jev-1.13.0" || decision.Answers["department"].Choice != "technical" {
				t.Errorf("decision = %#v", decision)
			}
			if metrics == nil || metrics.InputTokens != 392 || metrics.OutputTokens != 65 {
				t.Errorf("metrics = %#v", metrics)
			}
		})
	}
}

func TestClefErrorEnvelopeRendersMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"result":null,"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"messages":[]}`))
	}))
	defer srv.Close()

	t.Setenv("CLOUDFLARE_API_TOKEN", "bad-token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
	t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL)
	_, _, _, err := NewClefDecider().Decide(context.Background(), validDecisionRequest(), "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 401: Authentication error [code: 10000]") {
		t.Fatalf("error = %v", err)
	}
}

// TestClefFreeAllocationSpentIsNotRetried: once a Workers Free account has
// spent its 10,000 Neurons for the UTC day, every Clef call answers 429 code
// 4006 until 00:00 UTC. Read as a rate limit, doRequest backed off three
// times (~7 s per decider per call) before failing with the same message.
// It must fail on the first attempt, typed as billing through the decider's
// redaction wrappers, with Cloudflare's own remedy in the text.
func TestClefFreeAllocationSpentIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(workersAIFreeAllocationBody))
	}))
	defer srv.Close()

	t.Setenv("CLOUDFLARE_API_TOKEN", "test-cf-token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
	t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, _, err := NewClefDecider().Decide(ctx, validDecisionRequest(), "")
	if err == nil {
		t.Fatal("expected the free-allocation error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("a spent free allocation was sent %d times, want exactly 1", got)
	}
	for _, want := range []string{"HTTP 429", "daily free allocation of 10,000 neurons", "Workers Paid", "[code: 4006]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should carry Cloudflare's message (%q); got: %s", want, err)
		}
	}
	if !IsBillingError(err) || !IsPermanent(err) {
		t.Errorf("error is not a permanent *BillingError through the decider's wrappers: %T %v", err, err)
	}
}

func TestInvalidQuestionsRejectedBeforeNetwork(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("TYPESAFE_API_KEY", "test-typesafe-key")
	t.Setenv("CONCLAVE_JEV_BASE_URL", srv.URL)
	// noul returns a fresh valid question map so cases cannot share state.
	noul := func() map[string]Question {
		return map[string]Question{
			"q": {Type: QuestionNoul, Instructions: "Decide."},
		}
	}
	cases := map[string]struct {
		req  DecisionRequest
		want string
	}{
		"no questions": {DecisionRequest{State: "state"}, "questions"},
		"nil state":    {DecisionRequest{Questions: noul()}, "state"},
		"blank state":  {DecisionRequest{State: "   ", Questions: noul()}, "state"},
		"bad id": {DecisionRequest{State: "state", Questions: map[string]Question{
			"bad id!": {Type: QuestionNoul, Instructions: "Decide."},
		}}, "question \"bad id!\""},
		"id too long": {DecisionRequest{State: "state", Questions: map[string]Question{
			strings.Repeat("q", MaxQuestionIDLen+1): {Type: QuestionNoul, Instructions: "Decide."},
		}}, "id must be"},
		"missing instructions": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionNoul},
		}}, "instructions"},
		"unknown type": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: "boolean", Instructions: "Decide."},
		}}, "type"},
		"choice too small": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionChoice, Instructions: "Choose.", Choices: map[string]string{"a": "A"}},
		}}, "2..255 choices"},
		"empty choice label": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionChoice, Instructions: "Choose.", Choices: map[string]string{"": "A", "a": "B"}},
		}}, "labels must not be empty"},
		"choice with scale": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionChoice, Instructions: "Choose.", Choices: map[string]string{"a": "A", "b": "B"}, Scale: []string{"A", "B"}},
		}}, "score scale"},
		"score too small": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionScore, Instructions: "Score.", Scale: []string{"A"}},
		}}, "2..10 scale entries"},
		"score too large": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionScore, Instructions: "Score.", Scale: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}},
		}}, "2..10 scale entries"},
		"empty scale entry": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionScore, Instructions: "Score.", Scale: []string{"Low", " "}},
		}}, "scale entries must not be empty"},
		"score with choices": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionScore, Instructions: "Score.", Scale: []string{"A", "B"}, Choices: map[string]string{"a": "A", "b": "B"}},
		}}, "define choices"},
		"noul with criteria": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionNoul, Instructions: "Decide.", Scale: []string{"No", "Yes"}},
		}}, "noul criteria may only define"},
		"noul with unknown criterion": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionNoul, Instructions: "Decide.", NoulCriteria: map[string]string{"maybe": "x"}},
		}}, "got \"maybe\""},
		"noul with blank criterion": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionNoul, Instructions: "Decide.", NoulCriteria: map[string]string{"true": " "}},
		}}, "must not be empty"},
		"noul with empty criteria object": {DecisionRequest{State: "state", Questions: map[string]Question{
			"q": {Type: QuestionNoul, Instructions: "Decide.", Choices: map[string]string{}},
		}}, "noul criteria may only define"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := NewJevDecider().Decide(context.Background(), tc.req, "")
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v; want it to mention %q", err, tc.want)
			}
		})
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("server received %d requests", got)
	}
}

func TestQuestionCriteriaMarshalsByType(t *testing.T) {
	cases := map[string]struct {
		question     Question
		criteriaType string
	}{
		"choice": {Question{Type: QuestionChoice, Instructions: "Choose.", Choices: map[string]string{"a": "A", "b": "B"}}, "object"},
		"score":  {Question{Type: QuestionScore, Instructions: "Score.", Scale: []string{"Low", "High"}}, "array"},
		"noul":   {Question{Type: QuestionNoul, Instructions: "Decide."}, "absent"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(tc.question)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(body, &wire); err != nil {
				t.Fatalf("wire decode: %v", err)
			}
			criteria, present := wire["criteria"]
			switch tc.criteriaType {
			case "object":
				if !present || len(criteria) == 0 || criteria[0] != '{' {
					t.Errorf("criteria = %s", criteria)
				}
			case "array":
				if !present || len(criteria) == 0 || criteria[0] != '[' {
					t.Errorf("criteria = %s", criteria)
				}
			case "absent":
				if present {
					t.Errorf("unexpected criteria = %s", criteria)
				}
			}

			var roundTrip Question
			if err := json.Unmarshal(body, &roundTrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			roundTripBody, err := json.Marshal(roundTrip)
			if err != nil || string(roundTripBody) != string(body) {
				t.Errorf("round trip = %s, %v; want %s", roundTripBody, err, body)
			}
		})
	}
}

func TestParseAPIErrorOpenAIShapeStillWorks(t *testing.T) {
	err := parseAPIError(http.StatusBadRequest, []byte(`{"error":{"message":"bad parameter","code":"invalid_value","param":"model"}}`))
	for _, want := range []string{"HTTP 400", "bad parameter", "invalid_value", "model"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestSystemOneRequestUsesDefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != "jev-latest" {
			t.Errorf("model = %q", body.Model)
		}
		_, _ = w.Write([]byte(systemOneTestResponse))
	}))
	defer srv.Close()

	t.Setenv("TYPESAFE_API_KEY", "test-typesafe-key")
	t.Setenv("CONCLAVE_JEV_BASE_URL", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, _, err := NewJevDecider().Decide(ctx, validDecisionRequest(), ""); err != nil {
		t.Fatalf("Decide: %v", err)
	}
}

// pointDeciderAtServer wires a backend by name to an httptest server so
// response-contract tests can run the same scenario through Jev and Clef.
func pointDeciderAtServer(t *testing.T, backend, srvURL string) *systemOneDecider {
	t.Helper()
	switch backend {
	case "jev":
		t.Setenv("TYPESAFE_API_KEY", "test-typesafe-key")
		t.Setenv("CONCLAVE_JEV_BASE_URL", srvURL)
		return NewJevDecider()
	case "clef":
		t.Setenv("CLOUDFLARE_API_TOKEN", "test-cf-token")
		t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
		t.Setenv("CONCLAVE_CLEF_BASE_URL", srvURL)
		return NewClefDecider()
	}
	t.Fatalf("unknown test backend %q", backend)
	return nil
}

func TestClefHTTP200EnvelopeSuccessFalseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTTP 200 with success:false is a failure, not an empty success:
		// consensus would count this decider as having answered.
		_, _ = w.Write([]byte(`{"result":null,"success":false,"errors":[{"code":5006,"message":"model failed"}],"messages":[]}`))
	}))
	defer srv.Close()

	t.Setenv("CLOUDFLARE_API_TOKEN", "test-cf-token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
	t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL)
	_, _, _, err := NewClefDecider().Decide(context.Background(), validDecisionRequest(), "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 200: model failed [code: 5006]") {
		t.Fatalf("error = %v; want the envelope's failure rendered like an API error", err)
	}
}

func TestDecideAnswerlessBodyIsAnError(t *testing.T) {
	cases := map[string]struct {
		backend string
		body    string
	}{
		"jev empty object":  {"jev", `{}`},
		"clef empty result": {"clef", `{"result":{},"success":true,"errors":[],"messages":[]}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			decider := pointDeciderAtServer(t, tc.backend, srv.URL)
			_, _, _, err := decider.Decide(context.Background(), validDecisionRequest(), "")
			if err == nil || !strings.Contains(err.Error(), "no answers") {
				t.Fatalf("error = %v; want an answerless 2xx body to be an error", err)
			}
		})
	}
}

func TestDecideMissingOrMismatchedAnswersAreErrors(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"missing question id": {
			`{"model":"jev-1.13.0","answers":{"unrelated":{"type":"noul","noul":1}}}`,
			"missing answers for questions: department",
		},
		"answer type mismatch": {
			`{"model":"jev-1.13.0","answers":{"department":{"type":"noul","noul":1}}}`,
			"department (want choice, got noul)",
		},
	}
	for _, backend := range []string{"jev", "clef"} {
		for name, tc := range cases {
			t.Run(backend+"/"+name, func(t *testing.T) {
				body := tc.body
				if backend == "clef" {
					body = `{"result":` + body + `,"success":true,"errors":[],"messages":[]}`
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(body))
				}))
				defer srv.Close()

				decider := pointDeciderAtServer(t, backend, srv.URL)
				_, _, _, err := decider.Decide(context.Background(), validDecisionRequest(), "")
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v; want it to name the defect %q", err, tc.want)
				}
			})
		}
	}
}

// cancelFirstTripTransport fails every trip and cancels the context on the
// first, so doRequest stops after one attempt instead of sleeping through its
// backoff cadence in tests.
type cancelFirstTripTransport struct {
	cancel context.CancelFunc
	once   sync.Once
}

func (t *cancelFirstTripTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.once.Do(t.cancel)
	return nil, errors.New("dial refused")
}

func TestDecideTransportErrorRedactsAccountID(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "test-cf-token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct-SECRET-123")
	// Empty prefix forces the real account-scoped Cloudflare URL; the trip
	// never leaves the process because the transport fails immediately.
	t.Setenv("CONCLAVE_CLEF_BASE_URL", "")
	decider := NewClefDecider()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	decider.client = &http.Client{Transport: &cancelFirstTripTransport{cancel: cancel}}

	_, _, _, err := decider.Decide(ctx, validDecisionRequest(), "")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if msg := err.Error(); strings.Contains(msg, "acct-SECRET-123") || strings.Contains(msg, "test-cf-token") {
		t.Errorf("error leaks a secret: %s", msg)
	}
	if !strings.Contains(err.Error(), "/accounts/<account>/") {
		t.Errorf("error does not show the redacted account segment: %v", err)
	}
}

func TestDecideRejectsUnsafeModelID(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("CLOUDFLARE_API_TOKEN", "test-cf-token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
	t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL)
	// "?" and "#" in a model id would become query and fragment of the request
	// URL instead of path segments, silently changing the endpoint.
	_, _, _, err := NewClefDecider().Decide(context.Background(), validDecisionRequest(), "clef?x=1#frag")
	if err == nil || !strings.Contains(err.Error(), "model id") {
		t.Fatalf("error = %v; want the unsafe model id rejected locally", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server received %d requests", hits.Load())
	}
}

// Clef's published schema gives noul an optional {"true","false"} criteria
// object (probed 2026-10-03); any other shape is still an error.
func TestQuestionUnmarshalNoulCriteriaShapes(t *testing.T) {
	var ok Question
	if err := json.Unmarshal([]byte(`{"type":"noul","instructions":"Decide.","criteria":{"true":"spam","false":"not spam"}}`), &ok); err != nil {
		t.Fatalf("true/false criteria: %v", err)
	}
	if ok.NoulCriteria["true"] != "spam" || ok.NoulCriteria["false"] != "not spam" || ok.Choices != nil {
		t.Fatalf("decoded %#v", ok)
	}
	out, err := json.Marshal(ok)
	if err != nil || !strings.Contains(string(out), `"criteria":{"false":"not spam","true":"spam"}`) {
		t.Fatalf("round trip = %s, %v", out, err)
	}
	for name, body := range map[string]string{
		"array":  `{"type":"noul","instructions":"Decide.","criteria":[]}`,
		"string": `{"type":"noul","instructions":"Decide.","criteria":"yes"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var q Question
			if err := json.Unmarshal([]byte(body), &q); err == nil {
				t.Fatalf("decoded %#v; want non-object noul criteria to be an error", q)
			}
		})
	}
	var bad Question
	_ = json.Unmarshal([]byte(`{"type":"noul","instructions":"Decide.","criteria":{"a":"b"}}`), &bad)
	if err := ValidateDecisionRequest(DecisionRequest{State: "s", Questions: map[string]Question{"q": bad}}); err == nil {
		t.Fatal("noul criteria key \"a\" passed validation")
	}
}

// TestDecideVendorEchoRedactsEverySecret: a vendor error body that echoes the
// Authorization header, every rotator key and the account ids must reach the
// caller with each value replaced by <redacted>, for jev and clef alike; a 402
// echo must still classify as BillingError through the redacting wrapper.
func TestDecideVendorEchoRedactsEverySecret(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusPaymentRequired} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("rejected " + r.Header.Get("Authorization") + " KEY_ONE KEY_TWO ACCT_ONE ACCT_TWO"))
		}))
		t.Setenv("TYPESAFE_API_KEY", "KEY_ONE,KEY_TWO")
		t.Setenv("CONCLAVE_JEV_BASE_URL", srv.URL)
		t.Setenv("CLOUDFLARE_API_TOKEN", "KEY_ONE,KEY_TWO")
		t.Setenv("CLOUDFLARE_ACCOUNT_ID", "ACCT_ONE,ACCT_TWO")
		t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL)

		cases := map[string]*systemOneDecider{"jev": NewJevDecider(), "clef": NewClefDecider()}
		for name, d := range cases {
			_, _, _, err := d.Decide(context.Background(), validDecisionRequest(), "")
			if err == nil {
				t.Fatalf("%s %d: expected an error", name, status)
			}
			msg := err.Error()
			secrets := []string{"KEY_ONE", "KEY_TWO"}
			if name == "clef" {
				secrets = append(secrets, "ACCT_ONE", "ACCT_TWO")
			}
			for _, s := range secrets {
				if strings.Contains(msg, s) {
					t.Errorf("%s %d: error leaks %s: %s", name, status, s, msg)
				}
			}
			if !strings.Contains(msg, "<redacted>") {
				t.Errorf("%s %d: no <redacted> marker: %s", name, status, msg)
			}
			var billing *BillingError
			if status == http.StatusPaymentRequired && !errors.As(err, &billing) {
				t.Errorf("%s: redaction hid the BillingError: %v", name, err)
			}
		}
		srv.Close()
	}
}

// OpenRouter's Decisions API sends error.code as a number (probed 2026-10-03);
// a string-typed Code failed the whole decode and surfaced the raw JSON body.
func TestParseAPIErrorNumericCodeRendersMessage(t *testing.T) {
	err := parseAPIError(401, []byte(`{"error":{"message":"User not found.","code":401}}`))
	if err == nil || err.Error() != "HTTP 401: User not found. [code: 401]" {
		t.Fatalf("err = %v", err)
	}
}
