package cmd

// Tests for `conclave decide` at the boundary a user hits: the command's
// stdout, its returned error (and so its exit code), and how many requests
// reached the vendor. Every backend is an httptest server reached through the
// CONCLAVE_JEV_BASE_URL / CONCLAVE_CLEF_BASE_URL overrides; nothing leaves
// the machine.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/decide"
)

const decideTestAnswer = `{"model":"%s","answers":{"department":{"type":"choice","choice":"technical","confidence":0.78,"probabilities":{"technical":0.85,"billing":0.15}},"q":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1000000,"output_tokens":10}}`

const decideTestQuestions = `department:
  type: choice
  instructions: Which team?
  criteria:
    billing: Money
    technical: Bugs
`

// decideFake is one fake vendor host serving both backends. status 0 = 200.
type decideFake struct {
	hits      atomic.Int32
	jevStatus int
	cfStatus  int
	lastBody  atomic.Value // string: the most recent request body
}

func newDecideFake(t *testing.T) (*decideFake, *bytes.Buffer) {
	t.Helper()
	f := &decideFake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		f.lastBody.Store(string(body))
		status, model := f.jevStatus, "jev-1.13.0"
		if strings.HasPrefix(r.URL.Path, "/clef") {
			status, model = f.cfStatus, "@cf/cloudflare/clef"
		}
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
			return
		}
		answer := strings.Replace(decideTestAnswer, "%s", model, 1)
		if strings.HasPrefix(r.URL.Path, "/clef") {
			// Workers AI always wraps successes (Phase 0, live 2026-10-03).
			answer = `{"result":` + answer + `,"success":true,"errors":[],"messages":[]}`
		}
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("TYPESAFE_API_KEY", "test-typesafe")
	t.Setenv("CLOUDFLARE_API_TOKEN", "test-cf")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CONCLAVE_JEV_BASE_URL", srv.URL+"/jev")
	t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL+"/clef")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CONCLAVE_CACHE_TTL", "")

	old := []any{flagDecideQuestions, flagDecideAsk, flagDecideFiles, flagDecideJSON, flagDecideQuiet, flagDecideTimeout, flagDecideNoStdin, flagCache, flagNoCache}
	flagDecideQuestions, flagDecideAsk, flagDecideFiles = "", "", nil
	flagDecideJSON, flagDecideQuiet, flagDecideTimeout, flagDecideNoStdin = true, false, 10, true
	flagCache, flagNoCache = "", false
	t.Cleanup(func() {
		flagDecideQuestions, flagDecideAsk, flagDecideFiles = old[0].(string), old[1].(string), old[2].([]string)
		flagDecideJSON, flagDecideQuiet, flagDecideTimeout, flagDecideNoStdin = old[3].(bool), old[4].(bool), old[5].(int), old[6].(bool)
		flagCache, flagNoCache = old[7].(string), old[8].(bool)
	})

	var out bytes.Buffer
	decideCmd.SetOut(&out)
	t.Cleanup(func() { decideCmd.SetOut(nil) })
	return f, &out
}

func writeQuestions(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "q.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func decodeEnvelope(t *testing.T, out *bytes.Buffer) decide.Envelope {
	t.Helper()
	var env decide.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not the --json envelope: %v\n%s", err, out.String())
	}
	return env
}

// decide_all_deciders_fail_exits_1_with_envelope: a script reading $? must see
// failure, and one reading stdout must still get a parseable envelope.
func TestDecideAllDecidersFailExits1WithEnvelope(t *testing.T) {
	f, out := newDecideFake(t)
	f.jevStatus, f.cfStatus = http.StatusUnauthorized, http.StatusUnauthorized
	flagDecideQuestions = writeQuestions(t, decideTestQuestions)

	err := runDecide(decideCmd, []string{"jev,clef", "ticket text"})
	if code := exitCodeFor(err, false); code != 1 {
		t.Fatalf("exit code = %d (err %v), want 1", code, err)
	}
	env := decodeEnvelope(t, out)
	if env.Meta.Succeeded != 0 || env.Meta.Requested != 2 {
		t.Fatalf("meta = %+v", env.Meta)
	}
	for name, r := range env.Deciders {
		if r.Status != decide.StatusError || r.Error == "" {
			t.Errorf("%s = %+v, want an error entry", name, r)
		}
	}
}

// decide_one_decider_fails_still_reports_consensus: one vendor down must not
// sink the run; the survivor's answer is the consensus and the run exits 0.
func TestDecideOneDeciderFailsStillReportsConsensus(t *testing.T) {
	f, out := newDecideFake(t)
	f.jevStatus = http.StatusUnauthorized
	flagDecideQuestions = writeQuestions(t, decideTestQuestions)

	if err := runDecide(decideCmd, []string{"jev,clef", "ticket text"}); err != nil {
		t.Fatalf("one decider succeeded, but the run failed: %v", err)
	}
	env := decodeEnvelope(t, out)
	qc := env.Consensus["department"]
	if qc.Choice != "technical" || qc.Succeeded != 1 || qc.Requested != 2 {
		t.Fatalf("department consensus = %+v", qc)
	}
	if env.Deciders["jev"].Status != decide.StatusError || env.Deciders["clef"].Status != decide.StatusSuccess {
		t.Fatalf("deciders = %+v", env.Deciders)
	}
}

// decide_invalid_questions_spend_nothing: a choice with one criterion is a
// local error; no request may reach a vendor, priced per token.
func TestDecideInvalidQuestionsSpendNothing(t *testing.T) {
	f, out := newDecideFake(t)
	flagDecideQuestions = writeQuestions(t, "department:\n  type: choice\n  instructions: Which?\n  criteria:\n    billing: Money\n")

	if err := runDecide(decideCmd, []string{"jev,clef", "ticket text"}); err == nil {
		t.Fatal("a one-choice question was accepted")
	}
	if n := f.hits.Load(); n != 0 {
		t.Fatalf("invalid questions reached the vendor %d time(s)", n)
	}
	// The backend re-validates too, so hits alone cannot tell a command that
	// validated up front from one that dispatched a doomed panel: the latter
	// prints an all-failed envelope.
	if out.Len() != 0 {
		t.Fatalf("invalid questions still dispatched the panel: %s", out.String())
	}
}

// decide_cache_hit_reports_cached_and_costs_nothing: the second identical run
// must not call the vendor, must keep status "success", say cached:true, and
// charge $0 (ADR-011).
func TestDecideCacheHitReportsCachedAndCostsNothing(t *testing.T) {
	f, out := newDecideFake(t)
	flagDecideQuestions = writeQuestions(t, decideTestQuestions)
	flagCache = "24h"

	if err := runDecide(decideCmd, []string{"jev", "ticket text"}); err != nil {
		t.Fatal(err)
	}
	first := decodeEnvelope(t, out)
	if first.Meta.TotalCostUSD == nil || *first.Meta.TotalCostUSD <= 0 {
		t.Fatalf("live call was not priced: %+v", first.Meta)
	}
	hits := f.hits.Load()

	out.Reset()
	if err := runDecide(decideCmd, []string{"jev", "ticket text"}); err != nil {
		t.Fatal(err)
	}
	if f.hits.Load() != hits {
		t.Fatal("cached run called the vendor again")
	}
	env := decodeEnvelope(t, out)
	r := env.Deciders["jev"]
	if r.Status != decide.StatusSuccess || !r.Cached {
		t.Fatalf("cached result = %+v, want status success + cached", r)
	}
	if env.Meta.TotalCostUSD == nil || *env.Meta.TotalCostUSD != 0 {
		t.Fatalf("cache hit charged %v, want exactly 0", env.Meta.TotalCostUSD)
	}
}

// decide_ask_shorthand_builds_one_noul: --ask must send exactly one noul
// question with id "q" and report its consensus under that id.
func TestDecideAskShorthandBuildsOneNoul(t *testing.T) {
	f, out := newDecideFake(t)
	flagDecideAsk = "Is this urgent?"

	if err := runDecide(decideCmd, []string{"jev", "server down"}); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Questions map[string]struct {
			Type         string `json:"type"`
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(f.lastBody.Load().(string)), &sent); err != nil {
		t.Fatal(err)
	}
	q, ok := sent.Questions["q"]
	if len(sent.Questions) != 1 || !ok || q.Type != "noul" || q.Instructions != "Is this urgent?" {
		t.Fatalf("sent questions = %+v", sent.Questions)
	}
	env := decodeEnvelope(t, out)
	if qc := env.Consensus["q"]; qc.Noul == nil || *qc.Noul != 0.9 {
		t.Fatalf("q consensus = %+v", qc)
	}
}

// decide_rejects_provider_token: `conclave decide gemini ...` must refuse and
// name the deciders, never treat "gemini" as state or call anything.
func TestDecideRejectsProviderToken(t *testing.T) {
	f, _ := newDecideFake(t)
	flagDecideAsk = "Urgent?"

	for _, args := range [][]string{{"gemini", "ticket text"}, {"gemini,jev"}, {"claude@cli,clef", "ticket text"}} {
		err := runDecide(decideCmd, args)
		if err == nil || !strings.Contains(err.Error(), "jev") || !strings.Contains(err.Error(), "clef") {
			t.Fatalf("args %q: got %v, want an error naming the deciders", args, err)
		}
	}
	if n := f.hits.Load(); n != 0 {
		t.Fatalf("a provider token reached a vendor %d time(s)", n)
	}
}

// decide_malformed_200_is_a_failed_decider: an HTTP 200 whose body is not a
// usable decision (unparseable, or a Workers AI success:false envelope) must
// count as that decider failing, so a panel of only such vendors exits 1.
func TestDecideMalformed200IsAFailedDecider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/clef") {
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":7000,"message":"no route"}],"result":null}`))
			return
		}
		_, _ = w.Write([]byte(`<html>gateway hiccup</html>`))
	}))
	t.Cleanup(srv.Close)
	_, out := newDecideFake(t)
	t.Setenv("CONCLAVE_JEV_BASE_URL", srv.URL+"/jev")
	t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL+"/clef")
	flagDecideAsk = "Urgent?"

	err := runDecide(decideCmd, []string{"jev,clef", "ticket text"})
	if code := exitCodeFor(err, false); code != 1 {
		t.Fatalf("exit code = %d (err %v), want 1", code, err)
	}
	env := decodeEnvelope(t, out)
	for _, name := range []string{"jev", "clef"} {
		if r := env.Deciders[name]; r.Status != decide.StatusError || r.Error == "" {
			t.Errorf("%s = %+v, want an error entry", name, r)
		}
	}
}

// decide_vendor_echo_never_leaks_credentials: a vendor that echoes the request
// (Authorization header, account id) into its error body must not carry a
// configured secret into stdout, stderr or the returned error. Every key of a
// comma-separated rotator counts, not only the one sent.
func TestDecideVendorEchoNeverLeaksCredentials(t *testing.T) {
	secrets := []string{"JEV_SENTINEL_A", "JEV_SENTINEL_B", "CF_SENTINEL_A", "CF_SENTINEL_B", "ACCT_SENTINEL_A", "ACCT_SENTINEL_B"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// Echo what a careless vendor might: the header in use plus that
		// vendor's own credential context (every rotator key, and for clef the
		// account ids). A decider can only redact secrets it holds.
		echo := "request rejected: " + r.Header.Get("Authorization")
		if strings.HasPrefix(r.URL.Path, "/clef") {
			echo += " keys=CF_SENTINEL_A,CF_SENTINEL_B account=ACCT_SENTINEL_A,ACCT_SENTINEL_B"
		} else {
			echo += " keys=JEV_SENTINEL_A,JEV_SENTINEL_B"
		}
		_, _ = w.Write([]byte(echo))
	}))
	t.Cleanup(srv.Close)
	_, out := newDecideFake(t)
	t.Setenv("TYPESAFE_API_KEY", "JEV_SENTINEL_A, JEV_SENTINEL_B")
	t.Setenv("CLOUDFLARE_API_TOKEN", "CF_SENTINEL_A,CF_SENTINEL_B")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "ACCT_SENTINEL_A,ACCT_SENTINEL_B")
	t.Setenv("CONCLAVE_JEV_BASE_URL", srv.URL+"/jev")
	t.Setenv("CONCLAVE_CLEF_BASE_URL", srv.URL+"/clef")
	flagDecideAsk = "Urgent?"

	var runErr error
	stderr := captureStderr(t, func() {
		runErr = runDecide(decideCmd, []string{"jev,clef", "ticket text"})
	})
	if runErr == nil {
		t.Fatal("every vendor returned 400, but the run succeeded")
	}
	all := out.String() + "\n" + stderr + "\n" + runErr.Error()
	for _, s := range secrets {
		if strings.Contains(all, s) {
			t.Errorf("credential %s leaked:\n%s", s, all)
		}
	}
	// Decoded, because the envelope's JSON escapes "<" as <.
	for name, r := range decodeEnvelope(t, out).Deciders {
		if !strings.Contains(r.Error, "Bearer <redacted>") {
			t.Errorf("%s: echoed error lost its shape; want <redacted> in place of the secret: %q", name, r.Error)
		}
	}
}

// decide_typo_in_decider_list_is_rejected: a comma list with a misspelt entry
// must be refused, naming the bad entry and the valid ones, never decided on as
// state nor run with the entries that happen to resolve.
func TestDecideTypoInDeciderListIsRejected(t *testing.T) {
	f, out := newDecideFake(t)
	flagDecideAsk = "Urgent?"

	for _, args := range [][]string{{"jev,typo"}, {"jev,typo", "ticket text"}, {"jev,typo,clef-flsh", "ticket text"}} {
		err := runDecide(decideCmd, args)
		if err == nil || !strings.Contains(err.Error(), "typo") || !strings.Contains(err.Error(), "clef-flash") {
			t.Fatalf("args %q: got %v, want an error naming typo and the valid deciders", args, err)
		}
	}
	if n := f.hits.Load(); n != 0 || out.Len() != 0 {
		t.Fatalf("a bad decider list reached a vendor %d time(s): %s", n, out.String())
	}
}

// decide_single_word_state_is_state: one positional without a comma is the
// state, even when it spells a decider or a chat provider, and every
// configured decider answers it.
func TestDecideSingleWordStateIsState(t *testing.T) {
	f, out := newDecideFake(t)
	flagDecideAsk = "Urgent?"

	for _, word := range []string{"jev", "gemini"} {
		out.Reset()
		if err := runDecide(decideCmd, []string{word}); err != nil {
			t.Fatalf("decide %q: %v", word, err)
		}
		var sent struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal([]byte(f.lastBody.Load().(string)), &sent); err != nil {
			t.Fatal(err)
		}
		if sent.State != word {
			t.Fatalf("decide %q sent state %q", word, sent.State)
		}
		if env := decodeEnvelope(t, out); env.Meta.Requested != 3 {
			t.Fatalf("decide %q ran %d decider(s), want all 3", word, env.Meta.Requested)
		}
	}
}

// decide_comma_list_alone_needs_state: a lone comma list is the decider list,
// so with no stdin and no -f there is nothing to decide on.
func TestDecideCommaListAloneNeedsState(t *testing.T) {
	f, _ := newDecideFake(t)
	flagDecideAsk = "Urgent?"

	err := runDecide(decideCmd, []string{"jev,clef"})
	if err == nil || !strings.Contains(err.Error(), "no state") {
		t.Fatalf("got %v, want the no-state error", err)
	}
	if n := f.hits.Load(); n != 0 {
		t.Fatalf("reached a vendor %d time(s) with no state", n)
	}
}
