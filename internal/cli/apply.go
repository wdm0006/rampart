package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wdm0006/rampart/internal/github"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply branch protection rules to non-compliant repos",
	Long:  `Applies the branch protection rules defined in rampart.yaml to any repos that don't match the desired configuration.`,
	Run: func(cmd *cobra.Command, args []string) {
		owner, _ := cmd.Flags().GetString("owner")
		repo, _ := cmd.Flags().GetString("repo")
		exclude, _ := cmd.Flags().GetStringSlice("exclude")
		configPath, _ := cmd.Flags().GetString("config")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if owner == "" {
			user, err := github.GetCurrentUser()
			if err != nil {
				exitWithError(err.Error())
			}
			owner = user
		}

		results, cfg := auditRepos(owner, repo, configPath, exclude)

		// Find non-compliant repos
		var toUpdate []RepoAuditResult
		for _, r := range results {
			if !r.Compliant && !r.Skipped && r.Error == "" {
				toUpdate = append(toUpdate, r)
			}
		}

		if len(toUpdate) == 0 {
			fmt.Println("\nAll repos are compliant. Nothing to apply.")
			return
		}

		fmt.Printf("\n%d repo(s) to update:\n\n", len(toUpdate))

		updated := 0
		failed := 0
		for _, r := range toUpdate {
			if dryRun {
				fmt.Printf("  [dry-run] %s would be updated:\n", r.Repo)
				for _, d := range r.Diffs {
					if !d.Pass {
						fmt.Printf("      %s: %s → %s\n", d.Rule, d.Got, d.Want)
					}
				}
			} else {
				fmt.Printf("  Updating %s...", r.Repo)
				err := github.SetBranchProtection(owner, r.Repo, r.Branch, cfg.Rules)
				if err != nil {
					fmt.Printf(" failed: %s\n", err)
					failed++
				} else {
					fmt.Println(" done")
					updated++
				}
			}
		}

		fmt.Println()
		if dryRun {
			fmt.Printf("Dry run complete: %d repo(s) would be updated\n", len(toUpdate))
		} else {
			skipped := 0
			for _, r := range results {
				if r.Skipped {
					skipped++
				}
			}
			fmt.Printf("Results: %d updated, %d failed, %d skipped\n", updated, failed, skipped)
		}
	},
}

func init() {
	applyCmd.Flags().String("owner", "", "GitHub user or org to apply rules to (defaults to authenticated user)")
	applyCmd.Flags().String("repo", "", "Apply to a single repo instead of all repos")
	applyCmd.Flags().StringSlice("exclude", nil, "Repos to exclude (repeatable)")
	applyCmd.Flags().String("config", "rampart.yaml", "Path to config file")
	applyCmd.Flags().Bool("dry-run", false, "Preview changes without applying")
}
