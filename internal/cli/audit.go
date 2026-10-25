package cli

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.com/wdm0006/rampart/internal/config"
	"github.com/wdm0006/rampart/internal/github"
)

// RepoAuditResult holds the audit result for a single repo
type RepoAuditResult struct {
	Repo        string
	Branch      string
	Compliant   bool
	Diffs       []config.RuleDiff
	ActualRules config.Rules
	Error       string
	Skipped     bool
}

// progressOut receives the "Fetching"/"Auditing" progress lines; audit
// --format json redirects it to stderr so stdout stays valid JSON.
var progressOut io.Writer = os.Stdout

const defaultConcurrency = 4

var (
	getBranchProtection = github.GetBranchProtection
	getBranchRules      = github.GetBranchRules
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check repos against branch protection config",
	Long:  `Audits GitHub repos for a user or organization against the rules defined in rampart.yaml. Exits non-zero if any repos are non-compliant.`,
	Run: func(cmd *cobra.Command, args []string) {
		owner, _ := cmd.Flags().GetString("owner")
		repo, _ := cmd.Flags().GetString("repo")
		exclude, _ := cmd.Flags().GetStringSlice("exclude")
		configPath, _ := cmd.Flags().GetString("config")
		reportPath, _ := cmd.Flags().GetString("report")
		format, _ := cmd.Flags().GetString("format")
		concurrency, _ := cmd.Flags().GetInt("concurrency")
		baselinePath, _ := cmd.Flags().GetString("baseline")
		writeBaselinePath, _ := cmd.Flags().GetString("write-baseline")

		if err := validateConcurrency(concurrency); err != nil {
			exitWithError(err.Error())
		}
		if err := validateFormat(format); err != nil {
			exitWithError(err.Error())
		}
		if baselinePath != "" && writeBaselinePath != "" {
			exitWithError("--baseline and --write-baseline cannot be used together")
		}
		var base baselineFile
		if baselinePath != "" {
			var err error
			if base, err = loadBaseline(baselinePath); err != nil {
				exitWithError(err.Error())
			}
		}
		if format == formatJSON {
			progressOut = os.Stderr
		}

		if owner == "" {
			// Default to current user
			user, err := github.GetCurrentUser()
			if err != nil {
				exitWithError(err.Error())
			}
			owner = user
		}

		results, cfg := auditRepos(owner, repo, configPath, exclude, concurrency)
		var ev *baselineEval
		if baselinePath != "" {
			e := evaluateBaseline(results, base)
			ev = &e
		}
		appendStepSummary(owner, results, ev, os.Stderr)
		if writeBaselinePath != "" {
			if err := writeBaseline(writeBaselinePath, buildBaseline(results)); err != nil {
				exitWithError(err.Error())
			}
			fmt.Fprintf(os.Stderr, "Baseline written to %s\n", writeBaselinePath)
		}
		// With --write-baseline only read errors fail: they are never recorded.
		failing := auditFailing(results, ev, writeBaselinePath != "")

		if format == formatJSON {
			if err := writeAuditJSON(os.Stdout, buildAuditJSON(owner, configPath, cfg.Branch, results, ev)); err != nil {
				exitWithError(err.Error())
			}
			if reportPath != "" {
				data := newReportData(owner, configPath, cfg.Branch, results)
				if err := generateReport(reportPath, data); err != nil {
					exitWithError(err.Error())
				}
				fmt.Fprintf(os.Stderr, "Report written to %s\n", reportPath)
			}
			if failing {
				os.Exit(1)
			}
			return
		}

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
						tag := ""
						if ev.isBaselined(r.Repo, d.Rule) {
							tag = " (baselined)"
						}
						fmt.Printf("      %s: want %s, got %s%s\n", d.Rule, d.Want, d.Got, tag)
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
		if ev != nil {
			printBaselineText(os.Stdout, *ev)
		}

		if reportPath != "" {
			data := newReportData(owner, configPath, cfg.Branch, results)
			if err := generateReport(reportPath, data); err != nil {
				exitWithError(err.Error())
			}
			fmt.Printf("\nReport written to %s\n", reportPath)
		}

		if failing {
			os.Exit(1)
		}
	},
}

func init() {
	auditCmd.Flags().String("owner", "", "GitHub user or org to audit (defaults to authenticated user)")
	auditCmd.Flags().String("repo", "", "Audit a single repo instead of all repos")
	auditCmd.Flags().StringSlice("exclude", nil, "Repos to exclude (repeatable)")
	auditCmd.Flags().String("config", "rampart.yaml", "Path to config file")
	auditCmd.Flags().String("format", formatText, "Output format: text or json (json goes to stdout)")
	auditCmd.Flags().Int("concurrency", defaultConcurrency, "Number of repos to read in parallel (1 = serial)")
	auditCmd.Flags().String("baseline", "", "Only fail on drift not recorded in this baseline file (read errors always fail)")
	auditCmd.Flags().String("write-baseline", "", "Record current failing rules to this file; exits 1 only on read errors")
	auditCmd.Flags().String("report", "", "Write an HTML report to the given file path")
}

// auditRepos is the shared audit engine used by both audit and apply commands
func auditRepos(owner, repo, configPath string, exclude []string, concurrency int) ([]RepoAuditResult, config.Config) {
	cfg, err := config.Load(configPath)
	if err != nil {
		exitWithError(err.Error())
	}

	var repos []github.Repo
	if repo != "" {
		if cfg.Branch == "default" {
			r, err := github.GetRepo(owner, repo)
			if err != nil {
				exitWithError(err.Error())
			}
			repos = []github.Repo{r}
		} else {
			repos = []github.Repo{{Name: repo}}
		}
	} else {
		fmt.Fprintf(progressOut, "Fetching repos for %s...\n", owner)
		repos, err = github.ListRepos(owner)
		if err != nil {
			exitWithError(err.Error())
		}
	}

	excludeSet := make(map[string]bool)
	for _, e := range exclude {
		excludeSet[e] = true
	}

	fmt.Fprintf(progressOut, "Auditing %d repos against %s (branch: %s)\n\n", len(repos), configPath, cfg.Branch)

	return auditAll(owner, repos, cfg, excludeSet, concurrency), cfg
}

// auditAll audits repos with at most concurrency reads in flight. Results are
// placed by input index so output order matches the repo list for any value.
func auditAll(owner string, repos []github.Repo, cfg config.Config, excludeSet map[string]bool, concurrency int) []RepoAuditResult {
	results := make([]RepoAuditResult, len(repos))
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(repos) {
		concurrency = len(repos)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = auditOne(owner, repos[i], cfg, excludeSet)
			}
		}()
	}
	for i := range repos {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

// auditOne reads and compares a single repo.
func auditOne(owner string, r github.Repo, cfg config.Config, excludeSet map[string]bool) RepoAuditResult {
	if excludeSet[r.Name] {
		return RepoAuditResult{Repo: r.Name, Skipped: true, Error: "excluded"}
	}

	branch := cfg.Branch
	if branch == "default" {
		branch = r.DefaultBranch
	}

	// Get classic branch protection rules
	classicRules, ok, err := getBranchProtection(owner, r.Name, branch)
	if err != nil {
		return RepoAuditResult{Repo: r.Name, Error: err.Error()}
	}
	if !ok {
		return RepoAuditResult{Repo: r.Name, Skipped: true, Error: "insufficient permissions"}
	}

	// Get ruleset-based rules
	rulesetRules, err := getBranchRules(owner, r.Name, branch)
	if err != nil {
		return RepoAuditResult{Repo: r.Name, Branch: branch, Error: err.Error()}
	}

	// Merge: the effective protection is the most restrictive
	// combination of classic protection and rulesets
	actual := config.MergeProtective(classicRules, rulesetRules)

	effectiveRules := cfg.RulesForRepo(r.Name)
	diffs := config.Compare(effectiveRules, actual, cfg.AllowStricterRules)
	compliant := true
	for _, d := range diffs {
		if !d.Pass {
			compliant = false
			break
		}
	}

	return RepoAuditResult{
		Repo:        r.Name,
		Branch:      branch,
		Compliant:   compliant,
		Diffs:       diffs,
		ActualRules: actual,
	}
}

// validateConcurrency rejects values below 1.
func validateConcurrency(n int) error {
	if n < 1 {
		return fmt.Errorf("invalid --concurrency %d (must be >= 1)", n)
	}
	return nil
}

// auditFailing decides the audit exit status. With a baseline only new
// failures and read errors fail; when writing a baseline only read errors do,
// since they are never recorded.
func auditFailing(results []RepoAuditResult, ev *baselineEval, writingBaseline bool) bool {
	switch {
	case ev != nil:
		return ev.NewFailures > 0
	case writingBaseline:
		return readErrorCount(results) > 0
	default:
		return newReportData("", "", "", results).NonCompliant > 0
	}
}
