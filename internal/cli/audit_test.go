package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

func TestAuditReposRecordsBranchRulesReadError(t *testing.T) {
	originalProtection := getBranchProtection
	originalRules := getBranchRules
	t.Cleanup(func() {
		getBranchProtection = originalProtection
		getBranchRules = originalRules
	})

	getBranchProtection = func(_, _, _ string) (config.Rules, bool, error) {
		return config.NoProtectionRules(), true, nil
	}
	getBranchRules = func(_, _, _ string) (config.Rules, error) {
		return config.Rules{}, errors.New("insufficient permissions to read branch rules")
	}

	configPath := filepath.Join(t.TempDir(), "rampart.yaml")
	if err := os.WriteFile(configPath, []byte("branch: main\nrules: {}\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	results, _ := auditRepos("acme", "widget", configPath, nil)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	result := results[0]
	if result.Error != "insufficient permissions to read branch rules" {
		t.Errorf("Error = %q", result.Error)
	}
	if result.Compliant {
		t.Error("Compliant = true, want false")
	}
	if result.Diffs != nil {
		t.Errorf("Diffs = %#v, want nil because comparison must not run", result.Diffs)
	}
	if result.Branch != "main" {
		t.Errorf("Branch = %q, want main", result.Branch)
	}
}

func TestAuditReposCarriesMergedActualRules(t *testing.T) {
	originalProtection := getBranchProtection
	originalRules := getBranchRules
	t.Cleanup(func() {
		getBranchProtection = originalProtection
		getBranchRules = originalRules
	})

	classic := config.Rules{RequirePullRequest: true, RequiredApprovals: 2}
	ruleset := config.Rules{DismissStaleReviews: true}
	getBranchProtection = func(_, _, _ string) (config.Rules, bool, error) {
		return classic, true, nil
	}
	getBranchRules = func(_, _, _ string) (config.Rules, error) {
		return ruleset, nil
	}

	configPath := filepath.Join(t.TempDir(), "rampart.yaml")
	if err := os.WriteFile(configPath, []byte("branch: main\nrules: {}\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	results, _ := auditRepos("acme", "widget", configPath, nil)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	want := config.MergeProtective(classic, ruleset)
	if !reflect.DeepEqual(results[0].ActualRules, want) {
		t.Errorf("ActualRules = %#v, want %#v", results[0].ActualRules, want)
	}
}
