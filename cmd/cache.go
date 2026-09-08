package cmd

// `conclave cache` — inspect and empty the opt-in response cache written by
// --cache / CONCLAVE_CACHE_TTL. See internal/cache for the contract and
// ADR-011 for the decision.

import (
	"fmt"

	"github.com/0xDarkMatter/conclave-cli/internal/cache"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Inspect or empty the response cache",
	Long: `Conclave can reuse an identical provider response instead of paying for it
twice. The cache is OFF unless you ask for it with --cache[=TTL] on a query or
CONCLAVE_CACHE_TTL=<hours> in the environment.

An entry is addressed by the mode, provider, model and the full prompt
including any file or stdin context, so changing one byte of an attached file
is a miss. Judge synthesis is never cached.`,
}

var cacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show where the response cache lives and what it holds",
	Args:  cobra.NoArgs,
	RunE:  runCacheStats,
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete every cached response",
	Args:  cobra.NoArgs,
	RunE:  runCacheClear,
}

func init() {
	cacheCmd.AddCommand(cacheStatsCmd, cacheClearCmd)
	rootCmd.AddCommand(cacheCmd)
}

func runCacheStats(cmd *cobra.Command, args []string) error {
	c := cache.New("", 0)
	info, err := c.Stat()
	if err != nil {
		return fmt.Errorf("read response cache: %w", err)
	}

	fmt.Printf("Response cache\n\n")
	fmt.Printf("    %-12s %s\n", "Directory", info.Dir)
	fmt.Printf("    %-12s %d\n", "Entries", info.Entries)
	fmt.Printf("    %-12s %s\n", "Size", formatBytes(info.Bytes))
	if info.Entries > 0 {
		fmt.Printf("    %-12s %s\n", "Oldest", info.Oldest.Local().Format("2006-01-02 15:04"))
		fmt.Printf("    %-12s %s\n", "Newest", info.Newest.Local().Format("2006-01-02 15:04"))
	}
	fmt.Printf("    %-12s %s\n", "Default TTL", cache.DefaultTTL)
	fmt.Println()
	fmt.Println("Entries older than the TTL in force at query time are ignored, not deleted;")
	fmt.Println("run `conclave cache clear` to reclaim the space.")
	return nil
}

func runCacheClear(cmd *cobra.Command, args []string) error {
	c := cache.New("", 0)
	n, err := c.Clear()
	if err != nil {
		return fmt.Errorf("clear response cache: %w", err)
	}
	if n == 0 {
		fmt.Println("Response cache is already empty.")
		return nil
	}
	fmt.Printf("✓ Removed %d cached response(s) from %s\n", n, c.Dir())
	return nil
}

// formatBytes renders a byte count for the stats table.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
