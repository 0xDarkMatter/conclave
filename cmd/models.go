package cmd

// `conclave models` — inspect the cached OpenRouter pricing catalog and check
// conclave's compiled defaults against it. Read-only apart from the cache
// file. See internal/pricing for the contract and ADR-009 for the decision.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/spf13/cobra"
)

var (
	flagModelsRefresh bool
	flagModelsJSON    bool
	flagModelsCheck   bool
	flagModelsAll     bool
)

var modelsCmd = &cobra.Command{
	Use:   "models [provider]",
	Short: "Show current model ids and API prices from the OpenRouter catalog",
	Long: `Prints the models OpenRouter currently lists for each provider conclave
supports, with context length and pay-as-you-go API price (USD per million
tokens). The catalog is cached for ` + "`CONCLAVE_PRICING_TTL`" + ` hours (default 24)
under the user cache directory and refreshed in the background when stale.

Prices apply to API mode (-g / -c / --batch). CLI mode rides each provider's
subscription and pays nothing per token.

  conclave models                 # every provider, newest models first
  conclave models claude          # one provider
  conclave models --check         # verify compiled defaults still exist (exit 1 on drift)
  conclave models --refresh       # force a fetch now
  conclave models --json          # machine-readable dump of the cache`,
	Args: cobra.MaximumNArgs(1),
	RunE: runModels,
}

func init() {
	modelsCmd.Flags().BoolVar(&flagModelsRefresh, "refresh", false, "Fetch the catalog now instead of using the cache")
	modelsCmd.Flags().BoolVar(&flagModelsJSON, "json", false, "Print the cached catalog as JSON")
	modelsCmd.Flags().BoolVar(&flagModelsCheck, "check", false, "Check conclave's default and cheap models against the catalog; exit 1 if any are missing")
	modelsCmd.Flags().BoolVar(&flagModelsAll, "all", false, "Include OpenRouter-only variants (:free, :batch, ...)")
	rootCmd.AddCommand(modelsCmd)
}

func runModels(cmd *cobra.Command, args []string) error {
	if pricing.Disabled() {
		return fmt.Errorf("pricing catalog is disabled (CONCLAVE_NO_PRICING is set)")
	}
	cat, err := pricing.Load(cmd.Context(), pricing.Options{ForceRefresh: flagModelsRefresh})
	if cat == nil {
		if err == nil {
			err = fmt.Errorf("no catalog available")
		}
		return err
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	defer pricing.WaitBackground(3 * time.Second)

	if flagModelsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cat)
	}

	if flagModelsCheck {
		return checkDefaults(cat)
	}

	providersToShow := pricing.Providers()
	if len(args) == 1 {
		p := strings.ToLower(strings.TrimSpace(args[0]))
		if _, ok := pricing.VendorPrefix(p); !ok {
			return fmt.Errorf("unknown provider %q (known: %s)", p, strings.Join(pricing.Providers(), ", "))
		}
		providersToShow = []string{p}
	}

	age := time.Since(cat.FetchedAt).Round(time.Minute)
	staleNote := ""
	if cat.Stale {
		staleNote = " (stale, refreshing in background)"
	}
	fmt.Fprintf(os.Stdout, "OpenRouter catalog: %d models, fetched %s ago%s\n", len(cat.Models), age, staleNote)
	fmt.Fprintf(os.Stdout, "Prices are USD per 1M tokens, API mode only; CLI mode is subscription-billed.\n")

	cfg, _ := config.Load()
	for _, p := range providersToShow {
		models := cat.ByVendor(p)
		if flagModelsAll {
			models = allForVendor(cat, p)
		}
		// A vendor/model token (ADR-010) filters by its vendor; say so in the header.
		label := p
		if strings.Contains(p, "/") {
			label, _ = pricing.VendorPrefix(p)
			label += " (OpenRouter vendor)"
		}
		fmt.Fprintf(os.Stdout, "\n%s\n", strings.ToUpper(label))
		if len(models) == 0 {
			fmt.Fprintf(os.Stdout, "  (no models listed)\n")
			continue
		}
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "  MODEL\tCONTEXT\tIN $/M\tOUT $/M\tRELEASED\tNOTE\n")
		for _, m := range models {
			note := ""
			if cfg != nil {
				if cat.Has(p, cfg.Models[p]) && sameModel(cat, p, cfg.Models[p], m) {
					note = "default"
				} else if cat.Has(p, cfg.CheapModels[p]) && sameModel(cat, p, cfg.CheapModels[p], m) {
					note = "cheap"
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n",
				m.Slug(), fmtContext(m.ContextLength), fmtPrice(m.InputPerM), fmtPrice(m.OutputPerM),
				m.Created.Format("2006-01-02"), note)
		}
		tw.Flush()
	}
	return nil
}

// checkDefaults is the drift gate: every compiled default and cheap model must
// resolve in the catalog. Meant for CI and pre-release, hence the exit code.
func checkDefaults(cat *pricing.Catalog) error {
	cfg := config.DefaultConfig()
	type row struct{ provider, kind, model, status, hint string }
	var rows []row
	missing := 0
	check := func(kind string, m map[string]string) {
		names := make([]string, 0, len(m))
		for k := range m {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, p := range names {
			model := m[p]
			if entry, ok := cat.Lookup(p, model); ok {
				rows = append(rows, row{p, kind, model, "ok", fmt.Sprintf("%s $%s/$%s", entry.Slug(), fmtPrice(entry.InputPerM), fmtPrice(entry.OutputPerM))})
				continue
			}
			missing++
			hint := "not in catalog"
			if alts := cat.ByVendor(p); len(alts) > 0 {
				hint = "not in catalog; newest listed: " + alts[0].Slug()
			}
			rows = append(rows, row{p, kind, model, "MISSING", hint})
		}
	}
	check("default", cfg.Models)
	check("cheap", cfg.CheapModels)

	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "PROVIDER\tKIND\tMODEL\tSTATUS\tNOTE\n")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.provider, r.kind, r.model, r.status, r.hint)
	}
	tw.Flush()
	if missing > 0 {
		return fmt.Errorf("%d compiled model id(s) are not in the OpenRouter catalog; verify against the vendor and update internal/config/config.go + docs/MODEL_REGISTRY.md", missing)
	}
	fmt.Fprintf(os.Stdout, "\nAll compiled defaults resolve in the catalog (fetched %s).\n", cat.FetchedAt.Format("2006-01-02"))
	return nil
}

func sameModel(cat *pricing.Catalog, provider, id string, m pricing.Model) bool {
	entry, ok := cat.Lookup(provider, id)
	return ok && entry.ID == m.ID
}

func allForVendor(cat *pricing.Catalog, provider string) []pricing.Model {
	vendor, _ := pricing.VendorPrefix(provider)
	var out []pricing.Model
	for _, m := range cat.Models {
		if m.Vendor() == vendor {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

func fmtContext(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func fmtPrice(v float64) string {
	switch {
	case v == 0:
		return "0"
	case v < 0.1:
		return fmt.Sprintf("%.3f", v)
	case v < 10:
		return fmt.Sprintf("%.2f", v)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
