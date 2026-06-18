package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

var keyringCmd = &cobra.Command{
	Use:   "keyring",
	Short: "Manage API keys in the OS keyring",
	Long: `Store provider API keys in the OS keyring (Windows Credential Manager,
macOS Keychain, or the Secret Service on Linux) so they don't need to be
exported into the environment on every run.

Keys are stored under service "` + providers.KeyringService + `", keyed by the provider's
env-var name (e.g. GLM_API_KEY). When that env var is unset, conclave reads
the value from the keyring automatically.`,
}

var keyringSetCmd = &cobra.Command{
	Use:   "set <ENV_VAR>",
	Short: "Store a key in the OS keyring (reads the value from a hidden prompt or stdin)",
	Args:  cobra.ExactArgs(1),
	RunE:  runKeyringSet,
}

var keyringListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show which provider keys are stored in the keyring",
	Args:  cobra.NoArgs,
	RunE:  runKeyringList,
}

var keyringRmCmd = &cobra.Command{
	Use:   "rm <ENV_VAR>",
	Short: "Remove a key from the OS keyring",
	Args:  cobra.ExactArgs(1),
	RunE:  runKeyringRm,
}

func init() {
	keyringCmd.AddCommand(keyringSetCmd, keyringListCmd, keyringRmCmd)
	rootCmd.AddCommand(keyringCmd)
}

func runKeyringSet(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if name == "" {
		return fmt.Errorf("env var name must not be empty")
	}

	value, err := readSecret(fmt.Sprintf("%s: ", name))
	if err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("no value provided")
	}

	if err := keyring.Set(providers.KeyringService, name, value); err != nil {
		return fmt.Errorf("store in keyring: %w", err)
	}

	fmt.Printf("✓ Stored %s in the OS keyring (service %q). It loads automatically when %s is unset.\n",
		name, providers.KeyringService, name)
	return nil
}

func runKeyringList(cmd *cobra.Command, args []string) error {
	// Collect the canonical + alternate env-var names for every provider.
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, p := range providerSetups {
		add(p.envVar)
		add(p.altEnv)
	}

	fmt.Printf("Keyring entries (service %q):\n\n", providers.KeyringService)
	for _, n := range names {
		v, err := keyring.Get(providers.KeyringService, n)
		status := "—"
		if err == nil && strings.TrimSpace(v) != "" {
			status = "stored"
		}
		fmt.Printf("    %-22s [%s]\n", n, status)
	}
	return nil
}

func runKeyringRm(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if err := keyring.Delete(providers.KeyringService, name); err != nil {
		return fmt.Errorf("remove %s from keyring: %w", name, err)
	}
	fmt.Printf("✓ Removed %s from the OS keyring.\n", name)
	return nil
}

// readSecret reads a secret without echoing when stdin is a terminal; when
// stdin is piped (e.g. `echo $KEY | conclave keyring set ...`) it reads a line.
func readSecret(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Print(prompt)
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read secret: %w", err)
		}
		return string(b), nil
	}
	// Piped input — read a single line.
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read secret from stdin: %w", err)
	}
	return line, nil
}
