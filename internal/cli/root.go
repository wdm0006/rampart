package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "rampart",
	Short: "Audit and enforce GitHub branch protection rules",
	Long: `Rampart is a CLI tool that audits and manages GitHub branch protection
rules across all repos for a user or organization.

Define your desired protection rules in a YAML config file, then:
  - Run 'rampart audit' to check which repos are compliant
  - Run 'rampart apply' to fix non-compliant repos

Prerequisites:
  - GitHub CLI (gh) installed and authenticated
  - Admin access to the repos you want to manage`,
	Example: `  # Generate a default config
  rampart init

  # Audit all repos for a user
  rampart audit --owner myuser

  # Apply rules to non-compliant repos
  rampart apply --owner myuser

  # Preview changes without applying
  rampart apply --owner myuser --dry-run`,
}

// SetVersion sets the version string (called from main)
func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(applyCmd)
}

func exitWithError(msg string) {
	fmt.Fprintln(os.Stderr, "Error:", msg)
	os.Exit(1)
}
