package output

import (
	"embed"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed templates/*.md
var embeddedTemplates embed.FS

// ProviderInfo holds display info for a provider
type ProviderInfo struct {
	Name        string // short name (gemini, claude, etc.)
	Model       string // model name
	DisplayName string // full display: "Google Gemini 2.5 Pro"
}

// TemplateData holds all data available to templates
type TemplateData struct {
	// Header info
	Timestamp string // dd-mm-yyyy hh:mm format

	// Query info
	Query     string
	Providers []string
	JudgeName string

	// Provider details with display names
	ProviderList []ProviderInfo
	JudgeDisplay string // full judge display name

	// Settings
	Blind   bool
	Timeout int

	// Context info
	ContextSources []SourceInfo
	TotalBytes     int64
	ContextSummary string // e.g., "2 files (15.2 KB)"

	// Responses from each provider
	Responses []ResponseData

	// Verdict (nil if no judge)
	Verdict *VerdictData

	// Timing
	Timings      []TimingData
	TotalTime    string // total wall clock time

	// Aggregate metrics
	TotalTokens       int
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCostUSD      float64
	HasMetrics        bool

	// Pre-formatted header panel
	HeaderPanel string
}

type SourceInfo struct {
	Name      string
	Size      string
	Truncated bool
}

type MetricsData struct {
	InputTokens  int
	OutputTokens int
	CacheTokens  int
	CostUSD      float64
	HasMetrics   bool
}

type ResponseData struct {
	Provider string
	Model    string
	Status   string // "success" or "error"
	Response string
	Error    string
	Duration string
	Metrics  *MetricsData
}

type VerdictData struct {
	Result          string
	Confidence      string
	Reasoning       string
	Agreements      []string
	Disagreements   []string
	Recommendations []string
}

type TimingData struct {
	Provider string
	Duration string
}

// loadTemplate loads a template by name, checking user override first
func loadTemplate(name string) (*template.Template, error) {
	return loadTemplateWithFuncs(name, nil)
}

// loadTemplateWithFuncs loads a template with custom functions
func loadTemplateWithFuncs(name string, funcs template.FuncMap) (*template.Template, error) {
	// Check for user override
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}

	userTemplate := filepath.Join(configDir, "conclave", "templates", name)
	if data, err := os.ReadFile(userTemplate); err == nil {
		tmpl := template.New(name)
		if funcs != nil {
			tmpl = tmpl.Funcs(funcs)
		}
		return tmpl.Parse(string(data))
	}

	// Fall back to embedded template
	data, err := embeddedTemplates.ReadFile("templates/" + name)
	if err != nil {
		return nil, err
	}

	tmpl := template.New(name)
	if funcs != nil {
		tmpl = tmpl.Funcs(funcs)
	}
	return tmpl.Parse(string(data))
}
