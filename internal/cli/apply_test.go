package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
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

func TestClassifyApplyResults(t *testing.T) {
	compliant := RepoAuditResult{Repo: "ok", Compliant: true}
	drift := RepoAuditResult{Repo: "drift"}
	broken := RepoAuditResult{Repo: "broken", Error: "ruleset state unavailable"}
	excluded := RepoAuditResult{Repo: "excluded", Skipped: true}
	skippedWithError := RepoAuditResult{Repo: "noperm", Skipped: true, Error: "insufficient permissions"}

	tests := []struct {
		name         string
		results      []RepoAuditResult
		wantEligible []string
		wantErrors   []string
		wantSkipped  int
		wantFailed   bool
	}{
		{name: "all compliant", results: []RepoAuditResult{compliant, compliant}},
		{name: "all read errors", results: []RepoAuditResult{broken, broken}, wantErrors: []string{"broken", "broken"}, wantFailed: true},
		{name: "mixed", results: []RepoAuditResult{drift, broken, compliant}, wantEligible: []string{"drift"}, wantErrors: []string{"broken"}, wantFailed: true},
		{name: "skipped are not failures", results: []RepoAuditResult{excluded, skippedWithError, compliant}, wantSkipped: 2},
	}

	names := func(rs []RepoAuditResult) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Repo)
		}
		return out
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyApplyResults(tt.results)
			if g := names(got.Eligible); strings.Join(g, ",") != strings.Join(tt.wantEligible, ",") {
				t.Errorf("Eligible = %v, want %v", g, tt.wantEligible)
			}
			if g := names(got.ReadErrors); strings.Join(g, ",") != strings.Join(tt.wantErrors, ",") {
				t.Errorf("ReadErrors = %v, want %v", g, tt.wantErrors)
			}
			if got.Skipped != tt.wantSkipped {
				t.Errorf("Skipped = %d, want %d", got.Skipped, tt.wantSkipped)
			}
			if err := applyResultError(false, len(got.ReadErrors)); (err != nil) != tt.wantFailed {
				t.Errorf("exit error = %v, want failure %v", err, tt.wantFailed)
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

func TestApplyPostWriteVerification(t *testing.T) {
	for _, tt := range []struct {
		name         string
		after        int
		readError    bool
		skip         bool
		dryRun       bool
		restrictions bool
		stricter     bool
		want         string
		failed       bool
	}{
		{name: "residual drift", want: "required_approvals: 0 → 1", failed: true},
		{name: "verified", after: 1, want: "verified"},
		{name: "classic restrictions", after: 1, restrictions: true, want: "verified"},
		{name: "read error", readError: true, want: "verification read failed: unavailable", failed: true},
		{name: "skipped verification", skip: true, want: "verification read failed: insufficient permissions", failed: true},
		{name: "dry run", dryRun: true, want: "would be updated"},
		{name: "external protection", after: 2, stricter: true, want: "cannot converge", failed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf '[]'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			policy := "branch: main\nrules:\n  require_pull_request: true\n  required_approvals: 1\n"
			if tt.restrictions {
				policy += "  restrictions: {}\n"
			}
			cfgPath := filepath.Join(dir, "policy.yaml")
			if err := os.WriteFile(cfgPath, []byte(policy), 0644); err != nil {
				t.Fatal(err)
			}
			oldProtection, oldRules := getBranchProtection, getBranchRules
			t.Cleanup(func() { getBranchProtection, getBranchRules = oldProtection, oldRules })
			reads := 0
			getBranchProtection = func(owner, repo, branch string) (config.Rules, bool, error) {
				if owner != "acme" || repo != "widget" || branch != "main" {
					t.Errorf("unexpected audit target %s/%s %s", owner, repo, branch)
				}
				reads++
				actual := config.Rules{RequirePullRequest: true}
				if reads == 1 {
					if tt.stricter {
						actual.RequiredApprovals = 2
						actual.AllowForcePushes = true
					}
				} else {
					if tt.readError {
						return config.Rules{}, false, errors.New("unavailable")
					}
					if tt.skip {
						return config.Rules{}, false, nil
					}
					actual.RequiredApprovals = tt.after
					if tt.restrictions {
						actual.Restrictions = &config.Restrictions{}
					}
				}
				return actual, true, nil
			}
			getBranchRules = func(_, _, _ string) (config.Rules, error) { return config.NoProtectionRules(), nil }
			for flag, value := range map[string]string{"owner": "acme", "repo": "widget", "config": cfgPath, "dry-run": strconv.FormatBool(tt.dryRun), "concurrency": "1"} {
				f := applyCmd.Flags().Lookup(flag)
				old := f.Value.String()
				t.Cleanup(func() { _ = f.Value.Set(old) })
				if err := f.Value.Set(value); err != nil {
					t.Fatal(err)
				}
			}
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			oldStdout := os.Stdout
			os.Stdout = w
			err = applyCmd.RunE(applyCmd, nil)
			os.Stdout = oldStdout
			_ = w.Close()
			output, readErr := io.ReadAll(r)
			_ = r.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if (err != nil) != tt.failed {
				t.Errorf("error = %v, want failure %v", err, tt.failed)
			}
			if !strings.Contains(string(output), tt.want) {
				t.Errorf("output %q missing %q", output, tt.want)
			}
			if tt.failed && !strings.Contains(string(output), "Results: 0 updated, 1 failed, 0 skipped") {
				t.Errorf("incorrect counts: %s", output)
			}
			if !tt.failed && !tt.dryRun && !strings.Contains(string(output), "Results: 1 updated, 0 failed, 0 skipped") {
				t.Errorf("incorrect counts: %s", output)
			}
			wantReads := 2
			if tt.dryRun {
				wantReads = 1
			}
			if reads != wantReads {
				t.Errorf("reads = %d, want %d", reads, wantReads)
			}
		})
	}
}
