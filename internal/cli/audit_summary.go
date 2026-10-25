package cli

import (
	"fmt"
	"html"
	"io"
	"os"
	"strings"
)

const stepSummaryRowLimit = 50

func renderStepSummary(owner string, results []RepoAuditResult) string {
	return renderStepSummaryBaseline(owner, results, nil)
}

func renderStepSummaryBaseline(owner string, results []RepoAuditResult, ev *baselineEval) string {
	counts := jsonSummary{Total: len(results)}
	for _, r := range results {
		switch repoStatus(r) {
		case "skipped":
			counts.Skipped++
		case "compliant":
			counts.Compliant++
		default:
			counts.NonCompliant++
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\n### Rampart audit: %s\n\n", summaryCell(owner))
	fmt.Fprintf(&out, "%d compliant, %d non-compliant (including errors), %d skipped out of %d repos\n\n", counts.Compliant, counts.NonCompliant, counts.Skipped, counts.Total)
	if ev != nil {
		fmt.Fprintf(&out, "Baseline: %d baselined, %d new failures\n\n", ev.BaselinedCount, ev.NewFailures)
		for _, f := range ev.Fixed {
			fmt.Fprintf(&out, "- fixed - remove from baseline: %s\n", summaryCell(f))
		}
		if len(ev.Fixed) > 0 {
			out.WriteString("\n")
		}
	}
	rows := 0
	addRow := func(repo, rule, want, got string) {
		if rows == 0 {
			out.WriteString("| Repo | Rule | Want | Got |\n| --- | --- | --- | --- |\n")
		}
		if rows < stepSummaryRowLimit {
			fmt.Fprintf(&out, "| %s | %s | %s | %s |\n", summaryCell(repo), summaryCell(rule), summaryCell(want), summaryCell(got))
		}
		rows++
	}
	for _, r := range results {
		if r.Skipped {
			continue
		}
		if r.Error != "" {
			addRow(r.Repo, "error", "", r.Error)
		} else if !r.Compliant {
			for _, d := range r.Diffs {
				if !d.Pass {
					rule := d.Rule
					if ev.isBaselined(r.Repo, d.Rule) {
						rule += " (baselined)"
					}
					addRow(r.Repo, rule, d.Want, d.Got)
				}
			}
		}
	}
	if rows > stepSummaryRowLimit {
		fmt.Fprintf(&out, "\n+%d more rows\n", rows-stepSummaryRowLimit)
	}
	out.WriteString("\n")
	return out.String()
}

func summaryCell(value string) string {
	value = html.EscapeString(value)
	return strings.NewReplacer("\\", "\\\\", "|", "\\|", "\r\n", "<br>", "\r", "<br>", "\n", "<br>").Replace(value)
}

func appendStepSummary(owner string, results []RepoAuditResult, ev *baselineEval, warnings io.Writer) {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return
	}
	err := writeStepSummary(summaryPath, renderStepSummaryBaseline(owner, results, ev))
	if err != nil {
		fmt.Fprintf(warnings, "Warning: could not write GitHub Actions job summary: %v\n", err)
	}
}

func writeStepSummary(summaryPath, summary string) error {
	f, err := os.OpenFile(summaryPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(f, summary)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
