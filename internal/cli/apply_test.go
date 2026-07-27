package cli

import "testing"

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
