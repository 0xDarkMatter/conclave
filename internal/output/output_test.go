package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

func TestJSONOutput(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{JSON: true})
	result := Result{
		Query:     "test query",
		Providers: []string{"gemini", "openai"},
		JudgeName: "claude",
		Responses: []providers.Response{
			{
				Provider: "gemini",
				Model:    "gemini-2.5-pro",
				Status:   "success",
				Response: "test response",
				Duration: 2 * time.Second,
			},
		},
		Verdict: &judge.Verdict{
			Result:          "YES",
			Confidence:      "high",
			Reasoning:       "test reasoning",
			Agreements:      []string{"point 1"},
			Disagreements:   []string{},
			Recommendations: []string{"rec 1"},
			JudgeDuration:   1 * time.Second,
		},
	}

	err := formatter.Render(result)
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify it's valid JSON
	var jsonOut JSONOutput
	if err := json.Unmarshal([]byte(output), &jsonOut); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if jsonOut.Query != "test query" {
		t.Errorf("expected query 'test query', got %s", jsonOut.Query)
	}

	if jsonOut.Version != "1.0" {
		t.Errorf("expected version '1.0', got %s", jsonOut.Version)
	}

	if jsonOut.Verdict.Result != "YES" {
		t.Errorf("expected verdict 'YES', got %s", jsonOut.Verdict.Result)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"exact", 5, "exact"},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.max)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, result, tt.expected)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{500, "500B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.input)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"short text", 20, "short text"},
		{"this is a longer text that needs wrapping", 20, "this is a longer\ntext that needs\nwrapping"},
		{"", 20, ""},
	}

	for _, tt := range tests {
		result := wrapText(tt.input, tt.width)
		if result != tt.expected {
			t.Errorf("wrapText(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
		}
	}
}

func TestMin(t *testing.T) {
	if min(1, 2) != 1 {
		t.Error("min(1, 2) should be 1")
	}
	if min(5, 3) != 3 {
		t.Error("min(5, 3) should be 3")
	}
	if min(4, 4) != 4 {
		t.Error("min(4, 4) should be 4")
	}
}

func TestNewFormatter(t *testing.T) {
	opts := Options{
		JSON:    true,
		Verbose: true,
		Brief:   false,
		Quiet:   false,
	}

	f := New(opts)
	if f == nil {
		t.Error("New() should not return nil")
	}
}

func TestResultWithContext(t *testing.T) {
	ctx := &context.Context{
		Sources: []context.Source{
			{Name: "test.go", SizeBytes: 100},
		},
		TotalBytes: 100,
	}

	result := Result{
		Query:     "test",
		Context:   ctx,
		Providers: []string{"gemini"},
	}

	if result.Context.TotalBytes != 100 {
		t.Errorf("expected total bytes 100, got %d", result.Context.TotalBytes)
	}
}

// TestRenderStyled_AllErrorsRenderedOnTotalFailure verifies that when every
// provider failed, each error is shown in full. This is the regression for
// the "all providers failed → user only sees truncated spinner line" bug
// found via e2e testing of the original B1 fix.
func TestRenderStyled_AllErrorsRenderedOnTotalFailure(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{})
	marker1 := "ALL_FAIL_MARKER_GEMINI"
	marker2 := "ALL_FAIL_MARKER_OPENAI"
	result := Result{
		Query:     "test",
		Providers: []string{"gemini", "openai"},
		Responses: []providers.Response{
			{Provider: "gemini", Model: "x", Status: "error", Error: "HTTP 404: " + marker1},
			{Provider: "openai", Model: "y", Status: "error", Error: "HTTP 429: " + marker2},
		},
		// no Verdict — all-fail case
	}
	if err := formatter.Render(result); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	rendered := stripAnsiAndWrap(buf.String())

	for _, want := range []string{marker1, marker2} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %q in all-fail render; got:\n%s", want, rendered)
		}
	}
}

// TestRenderStyled_LongErrorNotTruncated verifies that a long auth/transport
// error string is preserved end-to-end in the styled output — the diagnostic
// info (e.g. param/code on a 400) is usually at the end. Regression for the
// user's "Post https://api.op..." truncation report.
func TestRenderStyled_LongErrorNotTruncated(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{})
	tailMarker := "DIAGNOSTIC_TAIL_END_MARKER_77"
	longError := "HTTP 400: Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead. [code: unsupported_parameter] [param: max_tokens] — additional context follows — " + tailMarker
	result := Result{
		Query:     "test",
		Providers: []string{"openai"},
		Responses: []providers.Response{
			{Provider: "openai", Model: "gpt-5.5", Status: "error", Error: longError, Duration: 0},
		},
		// no Verdict — forces provider responses block
	}
	if err := formatter.Render(result); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	// Lipgloss inserts ANSI escapes and line breaks; strip both for the contains check.
	rendered := stripAnsiAndWrap(buf.String())
	if !strings.Contains(rendered, tailMarker) {
		t.Errorf("long error tail dropped from styled output; rendered (stripped):\n%s", rendered)
	}
}

// stripAnsiAndWrap removes ANSI escape sequences and collapses whitespace so
// soft-wrapped text can be matched against the original error string.
func stripAnsiAndWrap(s string) string {
	// Strip ESC[ ... m sequences.
	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm'
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		out = append(out, s[i])
		i++
	}
	// Collapse newlines + indents that Lipgloss inserts during soft-wrap.
	return strings.Join(strings.Fields(string(out)), " ")
}

// TestRenderStyled_ParseErrorShowsProviderResponses mirrors the renderHuman
// test but exercises the Lipgloss path that real users actually see by default.
func TestRenderStyled_ParseErrorShowsProviderResponses(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{})
	uniqueMarker := "STYLED_UNIQUE_MARKER_99"
	result := Result{
		Query:     "test",
		Providers: []string{"openai"},
		JudgeName: "openai",
		Responses: []providers.Response{
			{
				Provider: "openai",
				Model:    "gpt-5.5",
				Status:   "success",
				Response: uniqueMarker,
				Duration: 1 * time.Second,
			},
		},
		Verdict: &judge.Verdict{
			Result:     "PARSE_ERROR",
			Confidence: "low",
			Reasoning:  "boom",
		},
	}
	if err := formatter.Render(result); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if !bytes.Contains(buf.Bytes(), []byte(uniqueMarker)) {
		t.Errorf("PARSE_ERROR in styled output should still surface raw provider output; got:\n%s", buf.String())
	}
}

// TestRenderRaw_IgnoresVerdict verifies that --raw output skips judge synthesis
// content even if a verdict is present in the Result (defense-in-depth: cmd
// implies --no-judge, but renderer should be self-contained).
func TestRenderRaw_IgnoresVerdict(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{Raw: true})
	result := Result{
		Responses: []providers.Response{
			{Provider: "openai", Model: "gpt-4o", Status: "success", Response: "raw body"},
		},
		Verdict: &judge.Verdict{
			Result:     "YES",
			Confidence: "high",
			Reasoning:  "verdict reasoning should be absent",
		},
	}
	if err := formatter.Render(result); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if bytes.Contains([]byte(out), []byte("verdict reasoning should be absent")) {
		t.Errorf("raw output leaked verdict reasoning:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("raw body")) {
		t.Errorf("raw output missing provider response:\n%s", out)
	}
}

// TestRenderRaw verifies the sentinel-separated output format for --raw.
func TestRenderRaw(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{Raw: true})
	result := Result{
		Query:     "test",
		Providers: []string{"openai", "claude"},
		Responses: []providers.Response{
			{Provider: "openai", Model: "gpt-5.5", Status: "success", Response: "alpha body", Duration: 1 * time.Second},
			{Provider: "claude", Model: "claude-opus", Status: "error", Error: "HTTP 401: bad key", Duration: 0},
		},
	}
	if err := formatter.Render(result); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	for _, want := range []string{
		"===PROVIDER:openai MODEL:gpt-5.5 STATUS:success===",
		"alpha body",
		"===PROVIDER:claude MODEL:claude-opus STATUS:error===",
		"HTTP 401: bad key",
		"===END===",
	} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("missing %q in raw output:\n%s", want, out)
		}
	}

	// Raw output should not include CONCLAVE header art.
	if bytes.Contains([]byte(out), []byte("CONCLAVE")) {
		t.Errorf("raw output should not include header art; got:\n%s", out)
	}
}

// TestRenderHuman_ParseErrorShowsProviderResponses verifies that when the
// judge fails to parse its synthesis, the raw provider outputs are still
// surfaced rather than swallowed. Regression test for the pmail bug report.
func TestRenderHuman_ParseErrorShowsProviderResponses(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{}) // not verbose
	uniqueMarker := "UNIQUE_PROVIDER_OUTPUT_MARKER_42"
	result := Result{
		Query:     "test",
		Providers: []string{"openai"},
		JudgeName: "openai",
		Responses: []providers.Response{
			{
				Provider: "openai",
				Model:    "gpt-5.5",
				Status:   "success",
				Response: uniqueMarker,
				Duration: 1 * time.Second,
			},
		},
		Verdict: &judge.Verdict{
			Result:     "PARSE_ERROR",
			Confidence: "low",
			Reasoning:  "Failed to parse judge response",
		},
	}

	// renderHuman is the non-styled path; force it by calling directly
	if err := formatter.renderHuman(result); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte(uniqueMarker)) {
		t.Errorf("PARSE_ERROR verdict should surface raw provider output; got:\n%s", output)
	}
}
