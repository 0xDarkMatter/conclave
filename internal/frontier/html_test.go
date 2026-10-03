// Tests for the HTML report and terminal rendering (ADR-018). Fixtures are
// hand-built Results: synthetic names and numbers, no upstream data (the
// Decision Index Space carries no licence). Golden files live in testdata/;
// regenerate with `go test ./internal/frontier -run Golden -update` and
// review the diff by eye before committing.
package frontier

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// decisionFixture covers every rendering branch: frontier, dominated,
// self-reported, unpriced, unscored, zero cost, and a source with a warning.
// One name carries markup, to prove external strings reach the page escaped.
func decisionFixture() Result {
	return Result{
		Kind: KindDecision, Axis: AxisDecision, CostBasis: CostInputPerM,
		Points: []Point{
			{ID: "jev", Name: "Jev", Kind: KindDecision, Score: f64p(57.91), Cost: f64p(0.042), CostBasis: CostInputPerM, LatencyMs: f64p(310), ECE: f64p(0.041)},
			{ID: "clef", Name: "Clef", Kind: KindDecision, Score: f64p(61.2), Cost: f64p(0.24), CostBasis: CostInputPerM, LatencyMs: f64p(1450), ECE: f64p(0.03), SelfReported: true},
			{ID: "clef-flash", Name: "Clef-flash", Kind: KindDecision, Score: f64p(49.5), Cost: f64p(0.09), CostBasis: CostInputPerM, LatencyMs: f64p(220), SelfReported: true},
			{ID: "vendor/kev-4b", Name: "Kev 4B", Kind: KindDecision, Score: f64p(44.1), Cost: f64p(0.05), CostBasis: CostInputPerM, LatencyMs: f64p(95)},
			{ID: "vendor/free", Name: "Free Tier", Kind: KindDecision, Score: f64p(30), Cost: f64p(0), CostBasis: CostInputPerM},
			{ID: "Board Only", Name: "Board Only", Kind: KindDecision, Score: f64p(52), CostBasis: CostInputPerM, LatencyMs: f64p(700)},
			{ID: "vendor/new", Name: `New <img src="https://evil.example/x.png">`, Kind: KindDecision, Cost: f64p(0.1), CostBasis: CostInputPerM},
		},
		Sources: []Source{
			{Name: "Decision Index", URL: "https://huggingface.co/spaces/example/data/index-v0.2.1.json", AsOf: "2026-09-28T00:00:00Z", Edition: "v0.2.1", Warning: "Decision Index mirror cross-check did not run"},
			{Name: "OpenRouter decision-model catalog", URL: "https://openrouter.ai/api/v1/models?output_modalities=decisions"},
		},
	}
}

func chatFixture() Result {
	r := Result{Kind: KindChat, Axis: AxisIntelligence, CostBasis: CostBlendPerM,
		Sources: []Source{{Name: "Artificial Analysis via OpenRouter", URL: "https://openrouter.ai/api/v1/benchmarks", AsOf: "2026-10-01", Citation: "Artificial Analysis, artificialanalysis.ai"}}}
	// 20 dominated rows behind a 3-point frontier: exercises the top-15 cap.
	r.Points = append(r.Points,
		Point{ID: "a/cheap", Name: "Cheap", Kind: KindChat, Score: f64p(40), Cost: f64p(0.1)},
		Point{ID: "a/mid", Name: "Mid", Kind: KindChat, Score: f64p(60), Cost: f64p(1)},
		Point{ID: "a/top", Name: "Top", Kind: KindChat, Score: f64p(75), Cost: f64p(12)},
		Point{ID: "a/none", Name: "No Score", Kind: KindChat, Cost: f64p(3)},
	)
	for i := 0; i < 20; i++ {
		r.Points = append(r.Points, Point{ID: fmt.Sprintf("b/d%02d", i), Name: fmt.Sprintf("Dominated %02d", i), Kind: KindChat,
			Score: f64p(float64(30 + i)), Cost: f64p(float64(13 + i))})
	}
	return r
}

func renderHTML(t *testing.T, r Result) string {
	t.Helper()
	var b bytes.Buffer
	if err := RenderHTML(&b, r); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	return b.String()
}

// html_report_has_no_external_references: the report must work offline and
// never fetch anything, so no src=, href= or url( may point off-page. The one
// allowance is the sources block, where each source URL is an <a href>
// anchor, and only an anchor.
func TestHTMLReportHasNoExternalReferences(t *testing.T) {
	for name, r := range map[string]Result{"decision": decisionFixture(), "chat": chatFixture()} {
		t.Run(name, func(t *testing.T) {
			page := renderHTML(t, r)
			start := strings.Index(page, `<section id="sources">`)
			end := strings.Index(page, `</section>`)
			if start < 0 || end < start {
				t.Fatalf("no sources section in the report")
			}
			outside, inside := page[:start]+page[end:], page[start:end]

			remote := regexp.MustCompile(`(?i)(?:\b(?:src|href|xlink:href|action|poster|srcset)\s*=\s*["']?\s*|url\(\s*["']?\s*|@import\s+["']?)(?:https?:)?//`)
			if m := remote.FindString(outside); m != "" {
				t.Fatalf("external reference outside the sources block: %q", m)
			}

			// Inside: every remote attribute must be an anchor's href.
			attr := regexp.MustCompile(`(?i)<(\w+)\b[^>]*?\s(src|href|srcset|poster)\s*=\s*["']?\s*(?:https?:)?//`)
			for _, m := range attr.FindAllStringSubmatch(inside, -1) {
				if !strings.EqualFold(m[1], "a") || !strings.EqualFold(m[2], "href") {
					t.Fatalf("remote %s on <%s> inside the sources block: only <a href> is allowed", m[2], m[1])
				}
			}
			if regexp.MustCompile(`(?i)url\(|@import`).MatchString(inside) {
				t.Fatalf("CSS reference inside the sources block")
			}
			// Positive control: the rule above is only meaningful if source
			// links really render as anchors.
			if !strings.Contains(inside, `<a href="`+r.Sources[0].URL+`">`) {
				t.Fatalf("source URL %q is not rendered as an anchor in the sources block", r.Sources[0].URL)
			}
		})
	}
}

// html_report_never_uses_arial: user rule. The font stack is explicit and
// non-Arial, and form controls (which do not inherit font-family) get it too.
func TestHTMLReportNeverUsesArial(t *testing.T) {
	page := renderHTML(t, decisionFixture())
	if strings.Contains(strings.ToLower(page), "arial") {
		t.Fatalf("report mentions Arial")
	}
	if !regexp.MustCompile(`--font:\s*"Inter"`).MatchString(page) {
		t.Fatalf("report does not declare the explicit font stack")
	}
	if !regexp.MustCompile(`button, select, input[^{]*\{[^}]*font-family:\s*var\(--font\)`).MatchString(page) {
		t.Fatalf("form controls do not get the explicit font stack")
	}
	if !regexp.MustCompile(`body\s*\{[^}]*background:`).MatchString(page) {
		t.Fatalf("body has no explicit background")
	}
}

// html_report_has_no_native_dialogs: user rule, never alert/confirm/prompt.
func TestHTMLReportHasNoNativeDialogs(t *testing.T) {
	page := renderHTML(t, decisionFixture())
	if m := regexp.MustCompile(`\b(alert|confirm|prompt)\s*\(`).FindString(page); m != "" {
		t.Fatalf("report calls a native dialog: %q", m)
	}
}

// The model name carrying markup must arrive as text, never as a live tag.
func TestHTMLReportEscapesExternalNames(t *testing.T) {
	page := renderHTML(t, decisionFixture())
	if strings.Contains(page, `<img`) {
		t.Fatalf("an external model name reached the page as markup")
	}
	if !strings.Contains(page, "New &lt;img") {
		t.Fatalf("escaped model name missing from the report")
	}
}

// html_report_golden: a fixed Result renders byte-for-byte the same page.
func TestHTMLReportGolden(t *testing.T) {
	for name, r := range map[string]Result{"decision": decisionFixture(), "chat": chatFixture()} {
		t.Run(name, func(t *testing.T) {
			got := renderHTML(t, r)
			path := filepath.Join("testdata", "report_"+name+".golden.html")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			// Git on Windows (autocrlf) may check out the golden AND the
			// embedded template with CRLF, so compare line endings neutrally.
			if strings.ReplaceAll(string(want), "\r\n", "\n") != strings.ReplaceAll(got, "\r\n", "\n") {
				t.Fatalf("report differs from %s; rerun with -update and review the diff", path)
			}
		})
	}
}

// The terminal report's contract: frontier first, dominated capped with a
// count of the rest, scored-but-unpriced kept apart from unscored, a legend
// whenever a self-reported row shows, and every source in the footer.
func TestRenderTextSections(t *testing.T) {
	var b bytes.Buffer
	if err := RenderText(&b, chatFixture()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"FRONTIER (3)", "DOMINATED (20)", "and 5 more", "UNSCORED (1", "No Score", "SOURCES", "as of 2026-10-01"} {
		if !strings.Contains(out, want) {
			t.Errorf("chat report lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Cheap") > strings.Index(out, "Top") {
		t.Errorf("frontier not in ascending cost order:\n%s", out)
	}

	b.Reset()
	if err := RenderText(&b, decisionFixture()); err != nil {
		t.Fatal(err)
	}
	out = b.String()
	for _, want := range []string{"Clef *", "self-reported", "UNPRICED", "Board Only", "LATENCY", "ECE", "edition v0.2.1", "1.4 s"} {
		if !strings.Contains(out, want) {
			t.Errorf("decision report lacks %q:\n%s", want, out)
		}
	}
	if ws := Warnings(decisionFixture()); len(ws) != 1 || !strings.Contains(ws[0], "cross-check") {
		t.Errorf("Warnings = %q, want the board's cross-check warning", ws)
	}
}
