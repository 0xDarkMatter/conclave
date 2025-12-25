package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"github.com/spf13/cobra"
)

// Provider setup info
type providerSetup struct {
	name    string
	envVar  string
	url     string
	altEnv  string // Alternative env var (e.g., GOOGLE_API_KEY for gemini)
}

var providerSetups = []providerSetup{
	{"gemini", "GEMINI_API_KEY", "https://aistudio.google.com/apikey", "GOOGLE_API_KEY"},
	{"openai", "OPENAI_API_KEY", "https://platform.openai.com/api-keys", ""},
	{"claude", "ANTHROPIC_API_KEY", "https://console.anthropic.com/settings/keys", ""},
	{"perplexity", "PERPLEXITY_API_KEY", "https://www.perplexity.ai/settings/api", ""},
	{"grok", "XAI_API_KEY", "https://console.x.ai", ""},
	{"glm", "ZHIPU_API_KEY", "https://open.bigmodel.cn/usercenter/apikeys", ""},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Configure API keys for providers",
	Long: `Interactive setup to configure API keys for LLM providers.

Keys are saved to ~/.config/conclave/.env and loaded automatically.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("🔧 Conclave Setup")
	fmt.Println()
	fmt.Println("Checking providers...")

	// Check current status
	var missing []providerSetup
	for _, p := range providerSetups {
		key := os.Getenv(p.envVar)
		if key == "" && p.altEnv != "" {
			key = os.Getenv(p.altEnv)
		}

		if key != "" {
			fmt.Printf("  ✓ %-12s %s set\n", p.name, p.envVar)
		} else {
			fmt.Printf("  ✗ %-12s %s not set\n", p.name, p.envVar)
			missing = append(missing, p)
		}
	}

	fmt.Println()

	if len(missing) == 0 {
		fmt.Println("All providers configured!")
		return nil
	}

	// Ask to configure
	fmt.Printf("Configure %d missing provider(s)? [Y/n] ", len(missing))
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	if response == "n" || response == "no" {
		fmt.Println("Skipped.")
		return nil
	}

	// Collect new keys
	newKeys := make(map[string]string)

	for _, p := range missing {
		fmt.Println()
		fmt.Printf("─── %s ───\n", strings.Title(p.name))
		fmt.Printf("Get key: %s\n", p.url)
		fmt.Printf("%s: ", p.envVar)

		key, _ := reader.ReadString('\n')
		key = strings.TrimSpace(key)

		if key == "" {
			fmt.Println("  ⊘ Skipped")
			continue
		}

		// Validate the key
		fmt.Print("  Validating... ")
		if err := validateAPIKey(p.name, p.envVar, key); err != nil {
			fmt.Printf("✗ %v\n", err)
			fmt.Println("  Key not saved. You can try again with 'conclave init'")
			continue
		}
		fmt.Println("✓ Valid")

		newKeys[p.envVar] = key
		// Also set in current process so subsequent validations work
		os.Setenv(p.envVar, key)
	}

	// Save keys
	if len(newKeys) > 0 {
		if err := config.SaveEnvFile(newKeys); err != nil {
			return fmt.Errorf("save keys: %w", err)
		}

		envPath, _ := config.EnvFilePath()
		fmt.Println()
		fmt.Printf("✓ Saved %d key(s) to %s\n", len(newKeys), envPath)
		fmt.Println("  Keys will load automatically on next run.")
	} else {
		fmt.Println()
		fmt.Println("No keys configured.")
	}

	return nil
}

// validateAPIKey checks if an API key is valid by making a minimal request
func validateAPIKey(provider, envVar, key string) error {
	// Temporarily set the key for validation
	oldKey := os.Getenv(envVar)
	os.Setenv(envVar, key)
	defer os.Setenv(envVar, oldKey)

	// Get the API provider
	var p providers.Provider
	switch provider {
	case "gemini":
		p = providers.NewGeminiAPIProvider()
	case "openai":
		p = providers.NewOpenAIAPIProvider()
	case "claude":
		p = providers.NewAnthropicAPIProvider()
	case "perplexity":
		p = providers.NewPerplexityAPIProvider()
	case "grok":
		p = providers.NewGrokAPIProvider()
	case "glm":
		p = providers.NewGLMAPIProvider()
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}

	// Make a minimal test request
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, _, _, err := p.Query(ctx, "Say 'ok' and nothing else.", p.DefaultModel())
	if err != nil {
		// Simplify error message
		errStr := err.Error()
		if strings.Contains(errStr, "401") || strings.Contains(errStr, "authentication") {
			return fmt.Errorf("invalid API key")
		}
		if strings.Contains(errStr, "403") {
			return fmt.Errorf("API key lacks permissions")
		}
		return fmt.Errorf("validation failed: %v", err)
	}

	return nil
}

// RunInitIfNeeded checks if any providers are available and runs init if not
// Returns true if init was run
func RunInitIfNeeded(general bool) bool {
	// Check if any providers are available
	if providers.AnyAvailable(general) {
		return false
	}

	fmt.Println()
	fmt.Println("No providers configured. Starting setup...")

	if err := runInit(nil, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Setup error: %v\n", err)
	}

	return true
}
