package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// renderJSONTo runs the JSON renderer and returns the decoded document.
// renderJSON writes to os.Stdout directly, so the pipe swap is unavoidable.
func renderJSONTo(t *testing.T, f *Formatter, r Result) JSONOutput {
	t.Helper()
	old := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw

	renderErr := f.Render(r)
	pw.Close()
	os.Stdout = old
	if renderErr != nil {
		t.Fatalf("Render: %v", renderErr)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(pr); err != nil {
		t.Fatal(err)
	}
	var out JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, buf.String())
	}
	return out
}

// TestJSONReportsTheTimeoutInForce defends against a machine-readable field
// that silently reads 0 whatever -t was given. A consumer reading
// execution.timeout_seconds to decide whether a slow provider was cut off
// would draw the wrong conclusion from a hardcoded zero.
func TestJSONReportsTheTimeoutInForce(t *testing.T) {
	result := Result{
		Query:     "q",
		Providers: []string{"openai"},
		Timeout:   45,
		Responses: []providers.Response{
			{Provider: "openai", Model: "m", Status: "success", Response: "a"},
		},
	}

	out := renderJSONTo(t, New(Options{JSON: true, Timeout: 45}), result)
	if out.Execution.TimeoutSeconds != 45 {
		t.Fatalf("timeout_seconds = %d, want 45", out.Execution.TimeoutSeconds)
	}
}

// TestJSONTimeoutFallsBackToTheResult: the value lives on both the formatter
// options and the Result, and a caller that fills only one must not get a 0.
func TestJSONTimeoutFallsBackToTheResult(t *testing.T) {
	result := Result{
		Query:     "q",
		Providers: []string{"openai"},
		Timeout:   30,
		Responses: []providers.Response{
			{Provider: "openai", Model: "m", Status: "success", Response: "a"},
		},
	}

	out := renderJSONTo(t, New(Options{JSON: true}), result)
	if out.Execution.TimeoutSeconds != 30 {
		t.Fatalf("timeout_seconds = %d, want 30 from the Result", out.Execution.TimeoutSeconds)
	}
}
