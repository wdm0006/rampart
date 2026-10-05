package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

const (
	formatText = "text"
	formatJSON = "json"
)

func validateFormat(format string) error {
	if format != formatText && format != formatJSON {
		return fmt.Errorf("invalid --format %q (must be %q or %q)", format, formatText, formatJSON)
	}
	return nil
}

type jsonDiff struct {
	Rule string `json:"rule"`
	Pass bool   `json:"pass"`
	Want string `json:"want"`
	Got  string `json:"got"`
}

type jsonRepo struct {
	Repo   string     `json:"repo"`
	Branch string     `json:"branch"`
	Status string     `json:"status"`
	Error  string     `json:"error"`
	Diffs  []jsonDiff `json:"diffs"`
}

type jsonSummary struct {
	Compliant    int `json:"compliant"`
	NonCompliant int `json:"non_compliant"`
	Skipped      int `json:"skipped"`
	Total        int `json:"total"`
}

type jsonAudit struct {
	Owner   string      `json:"owner"`
	Config  string      `json:"config"`
	Branch  string      `json:"branch"`
	Summary jsonSummary `json:"summary"`
	Repos   []jsonRepo  `json:"repos"`
}

func repoStatus(r RepoAuditResult) string {
	switch {
	case r.Skipped:
		return "skipped"
	case r.Error != "":
		return "error"
	case r.Compliant:
		return "compliant"
	default:
		return "non_compliant"
	}
}

func buildAuditJSON(owner, configPath, branch string, results []RepoAuditResult) jsonAudit {
	counts := newReportData(owner, configPath, branch, results)
	out := jsonAudit{
		Owner:  owner,
		Config: configPath,
		Branch: branch,
		Summary: jsonSummary{
			Compliant:    counts.Compliant,
			NonCompliant: counts.NonCompliant,
			Skipped:      counts.Skipped,
			Total:        counts.Total,
		},
		Repos: make([]jsonRepo, 0, len(results)),
	}
	for _, r := range results {
		repo := jsonRepo{
			Repo:   r.Repo,
			Branch: r.Branch,
			Status: repoStatus(r),
			Error:  r.Error,
			Diffs:  make([]jsonDiff, 0, len(r.Diffs)),
		}
		for _, d := range r.Diffs {
			repo.Diffs = append(repo.Diffs, jsonDiff{Rule: d.Rule, Pass: d.Pass, Want: d.Want, Got: d.Got})
		}
		out.Repos = append(out.Repos, repo)
	}
	return out
}

func writeAuditJSON(w io.Writer, a jsonAudit) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(a)
}
