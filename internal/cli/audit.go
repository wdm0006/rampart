package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wdm0006/rampart/internal/config"
	"github.com/wdm0006/rampart/internal/github"
)

// RepoAuditResult holds the audit result for a single repo
type RepoAuditResult struct {
	Repo      string
	Compliant bool
	Diffs     []config.RuleDiff
	Error     string
	Skipped   bool
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check repos against branch protection config",
	Long:  `Audits GitHub repos for a user or organization against the rules defined in rampart.yaml. Exits non-zero if any repos are non-compliant.`,
	Run: func(cmd *cobra.Command, args []string) {
		owner, _ := cmd.Flags().GetString("owner")
		repo, _ := cmd.Flags().GetString("repo")
		exclude, _ := cmd.Flags().GetStringSlice("exclude")
		configPath, _ := cmd.Flags().GetString("config")

		if owner == "" {
			// Default to current user
			user, err := github.GetCurrentUser()
			if err != nil {
				exitWithError(err.Error())
			}
			owner = user
		}

		results, _ := auditRepos(owner, repo, configPath, exclude)

		// Print results
		nonCompliant := 0
		for _, r := range results {
			if r.Skipped {
				fmt.Printf("  - %s (skipped: %s)\n", r.Repo, r.Error)
				continue
			}
			if r.Error != "" {
				fmt.Printf("  x %s (error: %s)\n", r.Repo, r.Error)
				nonCompliant++
				continue
			}
			if r.Compliant {
				fmt.Printf("  ✓ %s\n", r.Repo)
			} else {
				fmt.Printf("  ✗ %s\n", r.Repo)
				nonCompliant++
				for _, d := range r.Diffs {
					if !d.Pass {
						fmt.Printf("      %s: want %s, got %s\n", d.Rule, d.Want, d.Got)
					}
				}
			}
		}

		fmt.Println()
		total := len(results)
		compliant := total - nonCompliant
		skipped := 0
		for _, r := range results {
			if r.Skipped {
				skipped++
				compliant--
			}
		}
		fmt.Printf("Results: %d compliant, %d non-compliant, %d skipped out of %d repos\n",
			compliant, nonCompliant, skipped, total)

		if nonCompliant > 0 {
			os.Exit(1)
		}
	},
}

func init() {
	auditCmd.Flags().String("owner", "", "GitHub user or org to audit (defaults to authenticated user)")
	auditCmd.Flags().String("repo", "", "Audit a single repo instead of all repos")
	auditCmd.Flags().StringSlice("exclude", nil, "Repos to exclude (repeatable)")
	auditCmd.Flags().String("config", "rampart.yaml", "Path to config file")
}

// auditRepos is the shared audit engine used by both audit and apply commands
func auditRepos(owner, repo, configPath string, exclude []string) ([]RepoAuditResult, config.Config) {
	cfg, err := config.Load(configPath)
	if err != nil {
		exitWithError(err.Error())
	}

	var repos []github.Repo
	if repo != "" {
		repos = []github.Repo{{Name: repo}}
	} else {
		fmt.Printf("Fetching repos for %s...\n", owner)
		repos, err = github.ListRepos(owner)
		if err != nil {
			exitWithError(err.Error())
		}
	}

	excludeSet := make(map[string]bool)
	for _, e := range exclude {
		excludeSet[e] = true
	}

	fmt.Printf("Auditing %d repos against %s (branch: %s)\n\n", len(repos), configPath, cfg.Branch)

	var results []RepoAuditResult
	for _, r := range repos {
		if excludeSet[r.Name] {
			results = append(results, RepoAuditResult{
				Repo:    r.Name,
				Skipped: true,
				Error:   "excluded",
			})
			continue
		}

		actual, ok, err := github.GetBranchProtection(owner, r.Name, cfg.Branch)
		if err != nil {
			results = append(results, RepoAuditResult{
				Repo:  r.Name,
				Error: err.Error(),
			})
			continue
		}
		if !ok {
			results = append(results, RepoAuditResult{
				Repo:    r.Name,
				Skipped: true,
				Error:   "insufficient permissions",
			})
			continue
		}

		diffs := config.Compare(cfg.Rules, actual)
		compliant := true
		for _, d := range diffs {
			if !d.Pass {
				compliant = false
				break
			}
		}

		results = append(results, RepoAuditResult{
			Repo:      r.Name,
			Compliant: compliant,
			Diffs:     diffs,
		})
	}

	return results, cfg
}
