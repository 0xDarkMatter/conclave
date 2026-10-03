// Tests for `conclave models --frontier` (ADR-018): the key rule, flag
// refusals, and the advisory-source rule (one source down is a warning, all
// down is exit 3). Every source is an httptest fake reached through the
// packages' own overrides (CONCLAVE_OPENROUTER_MODELS_URL, the
// frontierBoardOptions seam); data is synthetic, never upstream's.
package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/benchmarks/decisionindex"
)

// frontierFlags sets the --frontier family for one test and restores every
// flag afterwards (they are package globals shared with other tests).
func frontierFlags(t *testing.T, deciders bool, axis, cost, html string) {
	t.Helper()
	flagModelsFrontier, flagModelsDeciders = true, deciders
	flagModelsAxis, flagModelsCost, flagModelsHTML = axis, cost, html
	t.Cleanup(func() {
		flagModelsFrontier, flagModelsDeciders = false, false
		flagModelsAxis, flagModelsCost, flagModelsHTML = "", "", ""
		flagModelsJSON = false
	})
}

// noKey makes the key lookup empty without touching the real keyring.
func noKey(t *testing.T) {
	t.Helper()
	old := frontierAPIKey
	frontierAPIKey = func() string { return "" }
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Cleanup(func() { frontierAPIKey = old })
}

// decisionSources serves a one-benchmark Decision Index edition (Jev 70,
// Kev 4B 60), a mirror that agrees and adds a self-reported Clef row, and
// the decision catalog. down names paths that answer 500 instead.
func decisionSources(t *testing.T, down ...string) {
	t.Helper()
	isDown := map[string]bool{}
	for _, d := range down {
		isDown[d] = true
	}
	// Edition identity is enforced by decisionindex.Load (ADR-018): the
	// methodology's edition.id and the index's suite.panel_id must match the
	// pinned edition, and the mirror's upstream.label must name it.
	meth := `{"edition":{"id":"` + decisionindex.Edition + `"},"benchmarks":[{"id":1,"tracks":[]}],"index":{"panel_id":"decision-index-0.2.1","areas":[{"id":"knowledge","weight":1,"panel":[{"id":1}]}],"chance_levels":[{"id":1,"chance":0}],"lower_rules":[]}}`
	idx := `{"generated_utc":"2026-09-28T00:00:00Z","suite":{"panel_id":"decision-index-0.2.1"},"models":[
	  {"engine":"jev","name":"Jev","benchmarks":{"1":{"raw":0.7,"coverage":1}},"latency":{"median":300}},
	  {"engine":"kev","name":"Kev 4B","benchmarks":{"1":{"raw":0.6,"coverage":1}},"latency":{"median":90}}]}`
	mirror := `{"upstream":{"label":"Decision Index 0.2.1"},"models":[{"engine":"jev","name":"Jev","index":70},{"engine":"kev","name":"Kev 4B","index":60},
	  {"engine":"clef","name":"Clef","index":65,"latency":{"median":1200}}]}`
	catalog := `{"data":[{"id":"typesafe/jev-1.13","name":"Jev","pricing":{"prompt":"0.000000042","completion":"0"}},
	  {"id":"jaredpalmer/kev-4b","name":"Kev 4B","pricing":{"prompt":"0.00000005","completion":"0"}}]}`
	bodies := map[string]string{
		"/data/" + decisionindex.MethodologyFile: meth,
		"/data/" + decisionindex.IndexFile:       idx,
		"/mirror":                                mirror,
		"/models":                                catalog,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok || isDown[r.URL.Path] {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("CONCLAVE_NO_PRICING", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CONCLAVE_OPENROUTER_MODELS_URL", srv.URL+"/models")
	old := frontierBoardOptions
	frontierBoardOptions = decisionindex.Options{
		HTTPClient:   srv.Client(),
		UpstreamBase: srv.URL + "/data/",
		MirrorURL:    srv.URL + "/mirror",
	}
	t.Cleanup(func() { frontierBoardOptions = old })
}

func runFrontierCapture(t *testing.T) (stdout, stderr string, code int) {
	t.Helper()
	var err error
	stderr = captureStderr(t, func() {
		stdout = captureStdout(t, func() { err = runModels(modelsCmd, nil) })
	})
	return stdout, stderr, exitCodeFor(err, false)
}

// frontier_chat_without_key_exits_3_and_names_the_variable: the benchmarks
// feed is keyed and is the chat frontier's only source, so no key is "no
// source at all" (exit 3), and the message must say which variable to set.
func TestFrontierChatWithoutKeyExits3AndNamesTheVariable(t *testing.T) {
	noKey(t)
	frontierFlags(t, false, "", "", "")
	var err error
	captureStdout(t, func() { err = runModels(modelsCmd, nil) })
	if code := exitCodeFor(err, false); code != ExitCatalogUnavailable {
		t.Fatalf("exit %d (%v), want %d", code, err, ExitCatalogUnavailable)
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("error does not name OPENROUTER_API_KEY: %v", err)
	}
}

// frontier_deciders_work_without_key: the board, mirror and decision catalog
// are public, so --deciders must succeed with no key, and --html must write
// the report and print its path.
func TestFrontierDecidersWorkWithoutKey(t *testing.T) {
	noKey(t)
	decisionSources(t)
	report := filepath.Join(t.TempDir(), "d.html")
	frontierFlags(t, true, "", "", report)

	out, errOut, code := runFrontierCapture(t)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	for _, want := range []string{"FRONTIER", "Jev", "Kev 4B", "Clef *", "self-reported", "Decision Index, as of 2026-09-28", "HTML report:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	page, err := os.ReadFile(report)
	if err != nil || !strings.Contains(string(page), "Kev 4B") {
		t.Fatalf("HTML report not written (%v)", err)
	}
}

// --json is the frontier.Result as is: pure JSON on stdout, kind and points
// intact, and the --html path moved to stderr so stdout stays parseable.
func TestFrontierJSONIsTheResult(t *testing.T) {
	noKey(t)
	decisionSources(t)
	frontierFlags(t, true, "", "", filepath.Join(t.TempDir(), "d.html"))
	flagModelsJSON = true

	out, errOut, code := runFrontierCapture(t)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	var res struct {
		Kind   string `json:"kind"`
		Points []struct {
			Name       string `json:"name"`
			OnFrontier bool   `json:"on_frontier"`
		} `json:"points"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	if res.Kind != "decision" || len(res.Points) == 0 || !res.Points[0].OnFrontier {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !strings.Contains(errOut, "HTML report:") {
		t.Fatalf("--html path not reported on stderr under --json:\n%s", errOut)
	}
}

// frontier_rejects_axis_with_deciders: decision models have one axis and one
// cost basis; a flag that would be ignored is an error, not a quiet override.
func TestFrontierRejectsAxisWithDeciders(t *testing.T) {
	cases := []struct {
		name               string
		deciders           bool
		axis, cost, wantIn string
	}{
		{"axis", true, "coding", "", "--axis"},
		{"axis named decision", true, "decision", "", "--axis"},
		{"cost task", true, "", "task", "--cost task"},
		{"cost blend", true, "", "blend", "--cost blend"},
		{"unknown chat axis", false, "vibes", "", "unknown --axis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frontierFlags(t, tc.deciders, tc.axis, tc.cost, "")
			err := runModels(modelsCmd, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("err = %v, want one mentioning %q", err, tc.wantIn)
			}
		})
	}
	t.Run("deciders without frontier", func(t *testing.T) {
		frontierFlags(t, true, "", "", "")
		flagModelsFrontier = false
		if err := runModels(modelsCmd, nil); err == nil || !strings.Contains(err.Error(), "--frontier") {
			t.Fatalf("err = %v, want one requiring --frontier", err)
		}
	})
}

// frontier_one_source_down_is_a_warning_not_an_error: the catalog failing
// leaves the board's scores standing (unpriced where only the catalog priced
// them) with a warning on stderr and exit 0; every source down is exit 3.
func TestFrontierOneSourceDownIsAWarningNotAnError(t *testing.T) {
	noKey(t)
	t.Run("catalog down", func(t *testing.T) {
		decisionSources(t, "/models")
		frontierFlags(t, true, "", "", "")
		out, errOut, code := runFrontierCapture(t)
		if code != 0 {
			t.Fatalf("exit %d with one source down, want 0; stderr:\n%s", code, errOut)
		}
		if !strings.Contains(errOut, "warning: OpenRouter decision-model catalog unavailable") {
			t.Fatalf("no warning naming the failed source on stderr:\n%s", errOut)
		}
		if !strings.Contains(out, "Kev 4B") || !strings.Contains(out, "UNPRICED") {
			t.Fatalf("board models missing or not marked unpriced:\n%s", out)
		}
	})
	t.Run("everything down", func(t *testing.T) {
		decisionSources(t, "/models", "/data/"+decisionindex.IndexFile, "/data/"+decisionindex.MethodologyFile, "/mirror")
		frontierFlags(t, true, "", "", "")
		_, errOut, code := runFrontierCapture(t)
		if code != ExitCatalogUnavailable {
			t.Fatalf("exit %d with no source at all, want %d; stderr:\n%s", code, ExitCatalogUnavailable, errOut)
		}
	})
}
