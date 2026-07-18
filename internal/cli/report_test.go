package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

func TestNewReportDataCounts(t *testing.T) {
	tests := []struct {
		name             string
		results          []RepoAuditResult
		wantTotal        int
		wantCompliant    int
		wantNonCompliant int
		wantSkipped      int
	}{
		{
			name:      "empty",
			results:   nil,
			wantTotal: 0,
		},
		{
			name: "mixed classifications",
			results: []RepoAuditResult{
				{Repo: "compliant-a", Compliant: true},
				{Repo: "compliant-b", Compliant: true},
				{Repo: "noncompliant", Compliant: false},
				{Repo: "errored", Compliant: false, Error: "boom"},
				{Repo: "skipped", Skipped: true, Error: "excluded"},
			},
			wantTotal:        5,
			wantCompliant:    2,
			wantNonCompliant: 2,
			wantSkipped:      1,
		},
		{
			name: "errored but compliant flag true still non-compliant",
			results: []RepoAuditResult{
				{Repo: "errored-compliant", Compliant: true, Error: "boom"},
			},
			wantTotal:        1,
			wantNonCompliant: 1,
		},
		{
			name: "skipped takes precedence over error",
			results: []RepoAuditResult{
				{Repo: "skipped", Skipped: true, Error: "insufficient permissions"},
			},
			wantTotal:   1,
			wantSkipped: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := newReportData("owner", "rampart.yaml", "main", tt.results)

			if data.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", data.Total, tt.wantTotal)
			}
			if data.Compliant != tt.wantCompliant {
				t.Errorf("Compliant = %d, want %d", data.Compliant, tt.wantCompliant)
			}
			if data.NonCompliant != tt.wantNonCompliant {
				t.Errorf("NonCompliant = %d, want %d", data.NonCompliant, tt.wantNonCompliant)
			}
			if data.Skipped != tt.wantSkipped {
				t.Errorf("Skipped = %d, want %d", data.Skipped, tt.wantSkipped)
			}
			if got := data.Compliant + data.NonCompliant + data.Skipped; got != data.Total {
				t.Errorf("Compliant+NonCompliant+Skipped = %d, want Total %d", got, data.Total)
			}
		})
	}
}

// TestNewReportDataMatchesAuditSummary asserts that the report summary counts
// agree with the audit command's own summary math (audit.go): an errored,
// non-skipped repo counts as non-compliant, and skipped repos are excluded
// from the compliant count.
func TestNewReportDataMatchesAuditSummary(t *testing.T) {
	results := []RepoAuditResult{
		{Repo: "ok-1", Compliant: true},
		{Repo: "ok-2", Compliant: true},
		{Repo: "drift", Compliant: false},
		{Repo: "errored", Compliant: false, Error: "boom"},
		{Repo: "excluded", Skipped: true, Error: "excluded"},
		{Repo: "no-perms", Skipped: true, Error: "insufficient permissions"},
	}

	data := newReportData("owner", "rampart.yaml", "main", results)

	// Replicate the audit.go summary math independently.
	total := len(results)
	auditNonCompliant := 0
	for _, r := range results {
		if r.Skipped {
			continue
		}
		if r.Error != "" || !r.Compliant {
			auditNonCompliant++
		}
	}
	auditSkipped := 0
	for _, r := range results {
		if r.Skipped {
			auditSkipped++
		}
	}
	auditCompliant := total - auditNonCompliant - auditSkipped

	if data.Total != total {
		t.Errorf("Total = %d, want %d", data.Total, total)
	}
	if data.NonCompliant != auditNonCompliant {
		t.Errorf("NonCompliant = %d, want audit summary %d", data.NonCompliant, auditNonCompliant)
	}
	if data.Skipped != auditSkipped {
		t.Errorf("Skipped = %d, want audit summary %d", data.Skipped, auditSkipped)
	}
	if data.Compliant != auditCompliant {
		t.Errorf("Compliant = %d, want audit summary %d", data.Compliant, auditCompliant)
	}
}

func TestGenerateReport(t *testing.T) {
	results := []RepoAuditResult{
		{Repo: "passing-repo", Branch: "main", Compliant: true},
		{
			Repo:      "failing-repo",
			Branch:    "main",
			Compliant: false,
			Diffs: []config.RuleDiff{
				{Rule: "required_approvals", Pass: false, Want: "2", Got: "1"},
			},
		},
		{Repo: "skipped-repo", Skipped: true, Error: "excluded"},
	}
	data := newReportData("acme", "rampart.yaml", "main", results)

	path := filepath.Join(t.TempDir(), "report.html")
	if err := generateReport(path, data); err != nil {
		t.Fatalf("generateReport returned error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}
	out := string(raw)

	wantContains := []string{
		"passing-repo",
		"failing-repo",
		"skipped-repo",
		"badge pass\">PASS",
		"badge fail\">FAIL",
		"badge skip\">SKIPPED",
		"required_approvals",
		// The failing rule's Want and Got should render in the diff table.
		"<td>2</td>",
		"<td>1</td>",
	}
	for _, want := range wantContains {
		if !strings.Contains(out, want) {
			t.Errorf("report output missing %q", want)
		}
	}
}
