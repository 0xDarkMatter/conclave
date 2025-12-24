{{if .Verdict}}
=================================================================
CONCLAVE VERDICT: {{.Verdict.Result}} ({{.Verdict.Confidence}} confidence)
=================================================================
{{else}}
=================================================================
CONCLAVE RESULTS
=================================================================
{{end}}

Query: {{.Query}}
{{if .ContextSources}}Context: {{range $i, $s := .ContextSources}}{{if $i}}, {{end}}{{$s.Name}} ({{$s.Size}}){{end}}
{{end}}Providers: {{range $i, $p := .Providers}}{{if $i}}, {{end}}{{$p}}{{end}}
{{if .Verdict}}Judge: {{.JudgeName}}
{{end}}
{{if .Verdict}}
REASONING:
{{.Verdict.Reasoning}}

{{if .Verdict.Agreements}}AGREEMENTS:
{{range .Verdict.Agreements}}  - {{.}}
{{end}}
{{end}}{{if .Verdict.Disagreements}}DISAGREEMENTS:
{{range .Verdict.Disagreements}}  - {{.}}
{{end}}
{{end}}{{if .Verdict.Recommendations}}RECOMMENDATIONS:
{{range $i, $r := .Verdict.Recommendations}}  {{add $i 1}}. {{$r}}
{{end}}
{{end}}{{end}}
{{range .Responses}}
-----------------------------------------------------------------
{{.Provider}} ({{.Model}}) - {{.Status}}{{if .Metrics}} | {{.Metrics.InputTokens}} in / {{.Metrics.OutputTokens}} out{{if hasCost .Metrics.CostUSD}} ${{printf "%.4f" .Metrics.CostUSD}}{{end}}{{end}}
-----------------------------------------------------------------
{{if eq .Status "success"}}{{.Response}}{{else}}Error: {{.Error}}{{end}}
{{end}}
-----------------------------------------------------------------
Completed | {{range $i, $t := .Timings}}{{if $i}}, {{end}}{{$t.Provider}}: {{$t.Duration}}{{end}}
{{if .HasMetrics}}Tokens: {{.TotalInputTokens}} in / {{.TotalOutputTokens}} out{{if hasCost .TotalCostUSD}} | Cost: ${{printf "%.4f" .TotalCostUSD}}{{end}}{{end}}
