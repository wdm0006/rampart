package config

import (
	"os"
	"testing"
)

func TestRulesForRepo_NoOverrides(t *testing.T) {
	cfg := Default()
	rules := cfg.RulesForRepo("any-repo")
	if rules.RequiredApprovals != 1 {
		t.Errorf("expected 1 approval, got %d", rules.RequiredApprovals)
	}
}

func TestRulesForRepo_ExactMatch(t *testing.T) {
	approvals := 3
	cfg := Default()
	cfg.Overrides = []Override{
		{
			Repos: []string{"special-repo"},
			Rules: OverrideRules{RequiredApprovals: &approvals},
		},
	}

	rules := cfg.RulesForRepo("special-repo")
	if rules.RequiredApprovals != 3 {
		t.Errorf("expected 3 approvals for special-repo, got %d", rules.RequiredApprovals)
	}

	rules = cfg.RulesForRepo("other-repo")
	if rules.RequiredApprovals != 1 {
		t.Errorf("expected 1 approval for other-repo, got %d", rules.RequiredApprovals)
	}
}

func TestRulesForRepo_GlobPattern(t *testing.T) {
	approvals := 2
	enforceAdmins := true
	cfg := Default()
	cfg.Rules.EnforceAdmins = false
	cfg.Overrides = []Override{
		{
			Repos: []string{"prod-*"},
			Rules: OverrideRules{
				RequiredApprovals: &approvals,
				EnforceAdmins:     &enforceAdmins,
			},
		},
	}

	rules := cfg.RulesForRepo("prod-api")
	if rules.RequiredApprovals != 2 {
		t.Errorf("expected 2 approvals for prod-api, got %d", rules.RequiredApprovals)
	}
	if !rules.EnforceAdmins {
		t.Error("expected enforce_admins=true for prod-api")
	}
	// Non-overridden field should inherit base
	if !rules.RequirePullRequest {
		t.Error("expected require_pull_request=true (inherited) for prod-api")
	}

	rules = cfg.RulesForRepo("staging-api")
	if rules.RequiredApprovals != 1 {
		t.Errorf("expected 1 approval for staging-api, got %d", rules.RequiredApprovals)
	}
}

func TestRulesForRepo_MultipleOverrides(t *testing.T) {
	approvals2 := 2
	approvals5 := 5
	cfg := Default()
	cfg.Overrides = []Override{
		{
			Repos: []string{"prod-*"},
			Rules: OverrideRules{RequiredApprovals: &approvals2},
		},
		{
			Repos: []string{"prod-critical"},
			Rules: OverrideRules{RequiredApprovals: &approvals5},
		},
	}

	// Both match: later override wins
	rules := cfg.RulesForRepo("prod-critical")
	if rules.RequiredApprovals != 5 {
		t.Errorf("expected 5 approvals for prod-critical, got %d", rules.RequiredApprovals)
	}

	// Only first matches
	rules = cfg.RulesForRepo("prod-api")
	if rules.RequiredApprovals != 2 {
		t.Errorf("expected 2 approvals for prod-api, got %d", rules.RequiredApprovals)
	}
}

func TestRulesForRepo_BoolOverrideFalse(t *testing.T) {
	nopr := false
	cfg := Default()
	cfg.Overrides = []Override{
		{
			Repos: []string{"docs-*"},
			Rules: OverrideRules{RequirePullRequest: &nopr},
		},
	}

	rules := cfg.RulesForRepo("docs-site")
	if rules.RequirePullRequest {
		t.Error("expected require_pull_request=false for docs-site")
	}
}

func TestLoadWithOverrides(t *testing.T) {
	content := `
branch: default
rules:
  required_approvals: 1
  require_pull_request: true
overrides:
  - repos: ["prod-*"]
    rules:
      required_approvals: 3
  - repos: ["docs-*"]
    rules:
      require_pull_request: false
`
	tmpFile, err := os.CreateTemp("", "rampart-test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Overrides) != 2 {
		t.Fatalf("expected 2 overrides, got %d", len(cfg.Overrides))
	}

	rules := cfg.RulesForRepo("prod-api")
	if rules.RequiredApprovals != 3 {
		t.Errorf("expected 3 approvals for prod-api, got %d", rules.RequiredApprovals)
	}
	if !rules.RequirePullRequest {
		t.Error("expected require_pull_request=true (inherited) for prod-api")
	}

	rules = cfg.RulesForRepo("docs-internal")
	if rules.RequirePullRequest {
		t.Error("expected require_pull_request=false for docs-internal")
	}
	if rules.RequiredApprovals != 1 {
		t.Errorf("expected 1 approval (inherited) for docs-internal, got %d", rules.RequiredApprovals)
	}
}

func TestNoProtectionRules(t *testing.T) {
	r := NoProtectionRules()
	if !r.AllowForcePushes {
		t.Error("expected AllowForcePushes=true for no protection")
	}
	if !r.AllowDeletions {
		t.Error("expected AllowDeletions=true for no protection")
	}
	if r.RequirePullRequest {
		t.Error("expected RequirePullRequest=false for no protection")
	}
}

func TestMergeProtective_BothEmpty(t *testing.T) {
	a := NoProtectionRules()
	b := NoProtectionRules()
	m := MergeProtective(a, b)
	if !m.AllowForcePushes {
		t.Error("expected AllowForcePushes=true when neither source blocks")
	}
	if !m.AllowDeletions {
		t.Error("expected AllowDeletions=true when neither source blocks")
	}
}

func TestMergeProtective_ClassicOnly(t *testing.T) {
	classic := Rules{
		RequirePullRequest:  true,
		RequiredApprovals:   1,
		DismissStaleReviews: true,
		EnforceAdmins:       true,
		AllowForcePushes:    false,
		AllowDeletions:      false,
		RequiredChecks:      []string{"build"},
	}
	rulesets := NoProtectionRules()
	m := MergeProtective(classic, rulesets)

	if !m.RequirePullRequest {
		t.Error("expected RequirePullRequest=true")
	}
	if m.RequiredApprovals != 1 {
		t.Errorf("expected 1 approval, got %d", m.RequiredApprovals)
	}
	if m.AllowForcePushes {
		t.Error("expected AllowForcePushes=false (classic blocks)")
	}
	if !m.EnforceAdmins {
		t.Error("expected EnforceAdmins=true")
	}
}

func TestMergeProtective_RulesetsOnly(t *testing.T) {
	classic := NoProtectionRules()
	rulesets := Rules{
		RequirePullRequest:             true,
		RequiredApprovals:              2,
		RequireCodeOwnerReviews:        true,
		AllowForcePushes:               false,
		AllowDeletions:                 true,
		RequiredConversationResolution: true,
		RequiredChecks:                 []string{"test"},
	}
	m := MergeProtective(classic, rulesets)

	if m.RequiredApprovals != 2 {
		t.Errorf("expected 2 approvals, got %d", m.RequiredApprovals)
	}
	if m.AllowForcePushes {
		t.Error("expected AllowForcePushes=false (rulesets block)")
	}
	if !m.AllowDeletions {
		t.Error("expected AllowDeletions=true (neither blocks)")
	}
	if !m.RequiredConversationResolution {
		t.Error("expected RequiredConversationResolution=true")
	}
}

func TestMergeProtective_Combined(t *testing.T) {
	classic := Rules{
		RequirePullRequest: true,
		RequiredApprovals:  1,
		EnforceAdmins:      true,
		AllowForcePushes:   true,
		AllowDeletions:     false,
		RequiredChecks:     []string{"build"},
	}
	rulesets := Rules{
		RequirePullRequest:      true,
		RequiredApprovals:       3,
		RequireCodeOwnerReviews: true,
		AllowForcePushes:        false,
		AllowDeletions:          true,
		RequiredChecks:          []string{"build", "lint"},
	}
	m := MergeProtective(classic, rulesets)

	// Most restrictive wins
	if m.RequiredApprovals != 3 {
		t.Errorf("expected 3 approvals (max), got %d", m.RequiredApprovals)
	}
	if !m.RequireCodeOwnerReviews {
		t.Error("expected RequireCodeOwnerReviews=true (from rulesets)")
	}
	if !m.EnforceAdmins {
		t.Error("expected EnforceAdmins=true (from classic)")
	}
	if m.AllowForcePushes {
		t.Error("expected AllowForcePushes=false (rulesets block)")
	}
	if m.AllowDeletions {
		t.Error("expected AllowDeletions=false (classic blocks)")
	}
	// Union of checks
	if len(m.RequiredChecks) != 2 {
		t.Errorf("expected 2 checks (union), got %v", m.RequiredChecks)
	}
}

func TestOverrideChecksSet(t *testing.T) {
	content := `
branch: default
rules:
  required_checks:
    - build
    - test
overrides:
  - repos: ["simple-*"]
    rules:
      required_checks: []
  - repos: ["extra-*"]
    rules:
      required_checks:
        - build
        - test
        - lint
  - repos: ["inherit-*"]
    rules:
      required_approvals: 2
`
	tmpFile, err := os.CreateTemp("", "rampart-test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Override with empty list should clear checks
	rules := cfg.RulesForRepo("simple-app")
	if len(rules.RequiredChecks) != 0 {
		t.Errorf("expected 0 checks for simple-app, got %v", rules.RequiredChecks)
	}

	// Override with explicit checks
	rules = cfg.RulesForRepo("extra-app")
	if len(rules.RequiredChecks) != 3 {
		t.Errorf("expected 3 checks for extra-app, got %v", rules.RequiredChecks)
	}

	// No checks override should inherit base
	rules = cfg.RulesForRepo("inherit-app")
	if len(rules.RequiredChecks) != 2 {
		t.Errorf("expected 2 checks (inherited) for inherit-app, got %v", rules.RequiredChecks)
	}
}
