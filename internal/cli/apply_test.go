package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestApplyResultError(t *testing.T) {
	tests := []struct {
		name    string
		dryRun  bool
		failed  int
		wantErr bool
	}{
		{name: "all updates succeed", wantErr: false},
		{name: "one update fails", failed: 1, wantErr: true},
		{name: "all updates fail", failed: 3, wantErr: true},
		{name: "dry run", dryRun: true, failed: 3, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := applyResultError(tt.dryRun, tt.failed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyResultError(%v, %d) error = %v, wantErr %v", tt.dryRun, tt.failed, err, tt.wantErr)
			}
		})
	}
}

func TestShouldApply(t *testing.T) {
	tests := []struct {
		name   string
		result RepoAuditResult
		want   bool
	}{
		{name: "non-compliant repository", result: RepoAuditResult{}, want: true},
		{name: "compliant repository", result: RepoAuditResult{Compliant: true}},
		{name: "skipped repository", result: RepoAuditResult{Skipped: true}},
		{name: "repository with read error", result: RepoAuditResult{Error: "ruleset state unavailable"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldApply(tt.result); got != tt.want {
				t.Errorf("shouldApply() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReportApplySuccess(t *testing.T) {
	tests := []struct {
		name          string
		unenforceable []string
		wantUpdated   int
		wantFailed    int
		wantOutput    []string
		wantExitError bool
	}{
		{
			name:        "fixable drift",
			wantUpdated: 1,
			wantOutput:  []string{"done"},
		},
		{
			name:          "stricter external protection",
			unenforceable: []string{"required_approvals", "dismiss_stale_reviews"},
			wantFailed:    1,
			wantOutput: []string{
				"required_approvals, dismiss_stale_reviews",
				"classic branch protection or another ruleset",
				"allow_stricter_rules: true",
			},
			wantExitError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, failed := applySuccessCounts(tt.unenforceable)
			if updated != tt.wantUpdated || failed != tt.wantFailed {
				t.Errorf("applySuccessCounts() = (%d, %d), want (%d, %d)", updated, failed, tt.wantUpdated, tt.wantFailed)
			}

			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			oldStdout := os.Stdout
			os.Stdout = write
			reportedUpdated, reportedFailed := reportApplySuccess(tt.unenforceable)
			os.Stdout = oldStdout
			if err := write.Close(); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(read)
			if err != nil {
				t.Fatal(err)
			}
			if err := read.Close(); err != nil {
				t.Fatal(err)
			}

			if reportedUpdated != updated || reportedFailed != failed {
				t.Errorf("reportApplySuccess() = (%d, %d), want (%d, %d)", reportedUpdated, reportedFailed, updated, failed)
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(string(output), want) {
					t.Errorf("output %q does not contain %q", output, want)
				}
			}
			if got := applyResultError(false, failed); (got != nil) != tt.wantExitError {
				t.Errorf("applyResultError(false, %d) = %v, want error %v", failed, got, tt.wantExitError)
			}
		})
	}
}
