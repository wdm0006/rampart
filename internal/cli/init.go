package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wdm0006/rampart/internal/config"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a default rampart.yaml config file",
	Long:  `Creates a rampart.yaml file in the current directory with sensible default branch protection rules.`,
	Run: func(cmd *cobra.Command, args []string) {
		path := "rampart.yaml"

		// Check if file already exists
		if _, err := os.Stat(path); err == nil {
			exitWithError(fmt.Sprintf("%s already exists", path))
		}

		if err := config.WriteDefault(path); err != nil {
			exitWithError(err.Error())
		}

		fmt.Printf("Created %s with default branch protection rules\n", path)
		fmt.Println("Edit the file to customize rules, then run: rampart audit --owner <name>")
	},
}
