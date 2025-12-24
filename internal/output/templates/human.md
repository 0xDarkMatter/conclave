{{.HeaderPanel}}
{{if .Verdict}}
══════════════════════════════════════════════════════════════════
 VERDICT: {{.Verdict.Result}} ({{.Verdict.Confidence}} confidence)
══════════════════════════════════════════════════════════════════

{{.Verdict.Reasoning}}

{{if .Verdict.Agreements}}AGREEMENTS:
{{range .Verdict.Agreements}}  • {{.}}
{{end}}
{{end}}{{if .Verdict.Disagreements}}DISAGREEMENTS:
{{range .Verdict.Disagreements}}  • {{.}}
{{end}}
{{end}}{{if .Verdict.Recommendations}}RECOMMENDATIONS:
{{range $i, $r := .Verdict.Recommendations}}  {{add $i 1}}. {{$r}}
{{end}}
{{end}}{{else}}{{range .Responses}}
──────────────────────────────────────────────────────────────────
{{.Provider}} ({{.Model}}) - {{.Status}}
──────────────────────────────────────────────────────────────────
{{if eq .Status "success"}}{{.Response}}{{else}}Error: {{.Error}}{{end}}
{{end}}{{end}}
──────────────────────────────────────────────────────────────────
Completed in {{.TotalTime}} | {{range $i, $t := .Timings}}{{if $i}}, {{end}}{{$t.Provider}}: {{$t.Duration}}{{end}}
{{if .HasMetrics}}Tokens: {{.TotalInputTokens}} in / {{.TotalOutputTokens}} out{{if hasCost .TotalCostUSD}} | Cost: ${{printf "%.4f" .TotalCostUSD}}{{end}}{{end}}
