package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

func TestRestrictions_LoadAndOverride(t *testing.T) {
	content := `
branch: default
rules:
  require_pull_request: true
  required_approvals: 0
  restrictions:
    users: [wdm0006]
    teams: []
    apps: [renovate]
overrides:
  - repos: ["public-*"]
    rules:
      restrictions:
        users: []
        teams: []
        apps: [dependabot]
  - repos: ["unrestricted-*"]
    rules:
      restrictions: null
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

	// Base rules have restrictions
	if cfg.Rules.Restrictions == nil {
		t.Fatal("expected base restrictions to be set")
	}
	if !reflect.DeepEqual(cfg.Rules.Restrictions.Users, []string{"wdm0006"}) {
		t.Errorf("expected base users=[wdm0006], got %v", cfg.Rules.Restrictions.Users)
	}
	if !reflect.DeepEqual(cfg.Rules.Restrictions.Apps, []string{"renovate"}) {
		t.Errorf("expected base apps=[renovate], got %v", cfg.Rules.Restrictions.Apps)
	}

	// Override fully replaces (does not merge)
	rules := cfg.RulesForRepo("public-site")
	if rules.Restrictions == nil {
		t.Fatal("expected restrictions on public-site")
	}
	if len(rules.Restrictions.Users) != 0 {
		t.Errorf("expected users=[] for public-site, got %v", rules.Restrictions.Users)
	}
	if !reflect.DeepEqual(rules.Restrictions.Apps, []string{"dependabot"}) {
		t.Errorf("expected apps=[dependabot] for public-site, got %v", rules.Restrictions.Apps)
	}

	// Explicit null clears the allowlist
	rules = cfg.RulesForRepo("unrestricted-app")
	if rules.Restrictions != nil {
		t.Errorf("expected restrictions=nil for unrestricted-app, got %+v", rules.Restrictions)
	}

	// Override without restrictions inherits base
	rules = cfg.RulesForRepo("inherit-app")
	if rules.Restrictions == nil {
		t.Fatal("expected restrictions inherited for inherit-app")
	}
	if !reflect.DeepEqual(rules.Restrictions.Users, []string{"wdm0006"}) {
		t.Errorf("expected inherited users=[wdm0006] for inherit-app, got %v", rules.Restrictions.Users)
	}
}

func TestRestrictions_ToAPIPayload(t *testing.T) {
	t.Run("nil_restrictions_serialize_to_null", func(t *testing.T) {
		r := Rules{}
		payload := r.ToAPIPayload()
		if payload["restrictions"] != nil {
			t.Errorf("expected restrictions=nil in payload, got %+v", payload["restrictions"])
		}

		// Confirm the JSON encodes as `"restrictions":null` so GitHub treats
		// it as "no allowlist" (the existing behavior).
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["restrictions"] != nil {
			t.Errorf("expected JSON restrictions=null, got %v", decoded["restrictions"])
		}
	})

	t.Run("populated_restrictions_serialize_to_object", func(t *testing.T) {
		r := Rules{
			Restrictions: &Restrictions{
				Users: []string{"wdm0006"},
				Teams: []string{},
				Apps:  []string{"renovate"},
			},
		}
		payload := r.ToAPIPayload()
		restr, ok := payload["restrictions"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected restrictions to be a map, got %T (%v)", payload["restrictions"], payload["restrictions"])
		}
		if !reflect.DeepEqual(restr["users"], []string{"wdm0006"}) {
			t.Errorf("expected users=[wdm0006], got %v", restr["users"])
		}
		if !reflect.DeepEqual(restr["apps"], []string{"renovate"}) {
			t.Errorf("expected apps=[renovate], got %v", restr["apps"])
		}
		// GitHub requires arrays (not null) for each principal type.
		if teams, ok := restr["teams"].([]string); !ok || len(teams) != 0 {
			t.Errorf("expected teams=[] (non-nil empty slice), got %v (%T)", restr["teams"], restr["teams"])
		}
	})
}

func TestRestrictions_RulesFromResponse(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		resp := ProtectionResponse{}
		r := RulesFromResponse(resp)
		if r.Restrictions != nil {
			t.Errorf("expected Restrictions=nil when API returned null, got %+v", r.Restrictions)
		}
	})

	t.Run("populated", func(t *testing.T) {
		raw := `{
			"enforce_admins": {"enabled": true},
			"allow_force_pushes": {"enabled": false},
			"allow_deletions": {"enabled": false},
			"required_linear_history": {"enabled": false},
			"required_conversation_resolution": {"enabled": false},
			"restrictions": {
				"users": [{"login": "wdm0006"}],
				"teams": [{"slug": "admins"}],
				"apps":  [{"slug": "renovate"}]
			}
		}`
		var resp ProtectionResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatal(err)
		}
		r := RulesFromResponse(resp)
		if r.Restrictions == nil {
			t.Fatal("expected Restrictions to be populated")
		}
		if !reflect.DeepEqual(r.Restrictions.Users, []string{"wdm0006"}) {
			t.Errorf("expected users=[wdm0006], got %v", r.Restrictions.Users)
		}
		if !reflect.DeepEqual(r.Restrictions.Teams, []string{"admins"}) {
			t.Errorf("expected teams=[admins], got %v", r.Restrictions.Teams)
		}
		if !reflect.DeepEqual(r.Restrictions.Apps, []string{"renovate"}) {
			t.Errorf("expected apps=[renovate], got %v", r.Restrictions.Apps)
		}
	})
}

func TestRestrictions_Compare(t *testing.T) {
	desired := Rules{
		Restrictions: &Restrictions{
			Users: []string{"wdm0006"},
			Apps:  []string{"renovate"},
		},
	}

	findDiff := func(diffs []RuleDiff, rule string) *RuleDiff {
		for i := range diffs {
			if diffs[i].Rule == rule {
				return &diffs[i]
			}
		}
		return nil
	}

	t.Run("no_actual_restrictions_fails", func(t *testing.T) {
		actual := Rules{}
		diffs := Compare(desired, actual, false)
		d := findDiff(diffs, "restrictions")
		if d == nil {
			t.Fatal("expected a restrictions diff")
		}
		if d.Pass {
			t.Errorf("expected restrictions diff to fail when actual has no allowlist, got %+v", d)
		}
	})

	t.Run("exact_match_passes", func(t *testing.T) {
		actual := Rules{
			Restrictions: &Restrictions{
				// Order swapped to exercise set-equality.
				Users: []string{"wdm0006"},
				Apps:  []string{"renovate"},
			},
		}
		diffs := Compare(desired, actual, false)
		d := findDiff(diffs, "restrictions")
		if d == nil || !d.Pass {
			t.Errorf("expected restrictions diff to pass for set-equal allowlists, got %+v", d)
		}
	})

	t.Run("subset_fails_without_allow_stricter", func(t *testing.T) {
		actual := Rules{
			Restrictions: &Restrictions{
				Users: []string{"wdm0006"},
				// Missing apps:[renovate]
			},
		}
		diffs := Compare(desired, actual, false)
		d := findDiff(diffs, "restrictions")
		if d == nil || d.Pass {
			t.Errorf("expected restrictions diff to fail when allow_stricter=false, got %+v", d)
		}
	})

	t.Run("subset_passes_with_allow_stricter", func(t *testing.T) {
		actual := Rules{
			Restrictions: &Restrictions{
				Users: []string{"wdm0006"},
			},
		}
		diffs := Compare(desired, actual, true)
		d := findDiff(diffs, "restrictions")
		if d == nil || !d.Pass {
			t.Errorf("expected restrictions diff to pass when actual is subset and allow_stricter=true, got %+v", d)
		}
	})

	t.Run("superset_fails_with_allow_stricter", func(t *testing.T) {
		// Actual has MORE entries → less strict → should fail even with
		// allow_stricter, because a larger allowlist is more permissive.
		actual := Rules{
			Restrictions: &Restrictions{
				Users: []string{"wdm0006", "someone-else"},
				Apps:  []string{"renovate"},
			},
		}
		diffs := Compare(desired, actual, true)
		d := findDiff(diffs, "restrictions")
		if d == nil || d.Pass {
			t.Errorf("expected restrictions diff to fail when actual is superset, got %+v", d)
		}
	})

	t.Run("omitted_in_desired_skips_diff", func(t *testing.T) {
		// When the user hasn't configured restrictions, audit should not
		// flag a repo even if it happens to have an allowlist set.
		desired := Rules{}
		actual := Rules{
			Restrictions: &Restrictions{Users: []string{"someone"}},
		}
		diffs := Compare(desired, actual, false)
		if findDiff(diffs, "restrictions") != nil {
			t.Error("expected no restrictions diff when desired.Restrictions is nil")
		}
	})
}

func TestRestrictions_MergeProtective(t *testing.T) {
	t.Run("nil_and_set_yields_set", func(t *testing.T) {
		a := Rules{}
		b := Rules{Restrictions: &Restrictions{Users: []string{"u"}}}
		m := MergeProtective(a, b)
		if m.Restrictions == nil {
			t.Fatal("expected merged restrictions to be the non-nil side")
		}
		if !reflect.DeepEqual(m.Restrictions.Users, []string{"u"}) {
			t.Errorf("expected users=[u], got %v", m.Restrictions.Users)
		}
	})

	t.Run("intersects_when_both_set", func(t *testing.T) {
		a := Rules{Restrictions: &Restrictions{
			Users: []string{"alice", "bob"},
			Apps:  []string{"renovate"},
		}}
		b := Rules{Restrictions: &Restrictions{
			Users: []string{"bob", "carol"},
			Apps:  []string{"renovate", "dependabot"},
		}}
		m := MergeProtective(a, b)
		got := m.Restrictions.Users
		sort.Strings(got)
		if !reflect.DeepEqual(got, []string{"bob"}) {
			t.Errorf("expected intersection users=[bob], got %v", m.Restrictions.Users)
		}
		if !reflect.DeepEqual(m.Restrictions.Apps, []string{"renovate"}) {
			t.Errorf("expected intersection apps=[renovate], got %v", m.Restrictions.Apps)
		}
	})

	t.Run("both_nil_stays_nil", func(t *testing.T) {
		m := MergeProtective(Rules{}, Rules{})
		if m.Restrictions != nil {
			t.Errorf("expected nil restrictions, got %+v", m.Restrictions)
		}
	})
}

// TestCompare exercises the general bool/approvals/checks paths of Compare for
// both allowStricter values. Each case asserts on the Pass field of specific
// RuleDiff entries (looked up by Rule name) rather than overall compliance.
func TestCompare(t *testing.T) {
	findDiff := func(diffs []RuleDiff, rule string) *RuleDiff {
		for i := range diffs {
			if diffs[i].Rule == rule {
				return &diffs[i]
			}
		}
		return nil
	}

	// A fully-protected desired config that touches every rule category.
	fullDesired := Rules{
		RequirePullRequest:             true,
		RequiredApprovals:              2,
		DismissStaleReviews:            true,
		RequireCodeOwnerReviews:        true,
		RequireStatusChecks:            true,
		StrictStatusChecks:             true,
		RequiredChecks:                 []string{"build", "test"},
		EnforceAdmins:                  true,
		AllowForcePushes:               false,
		AllowDeletions:                 false,
		RequiredLinearHistory:          true,
		RequiredConversationResolution: true,
	}

	tests := []struct {
		name          string
		desired       Rules
		actual        Rules
		allowStricter bool
		// want maps rule name -> expected Pass value. Only listed rules are checked.
		want map[string]bool
		// absent lists rule names that must NOT appear in the diff.
		absent []string
	}{
		{
			name:          "exact_match_passes_strict_false",
			desired:       fullDesired,
			actual:        fullDesired,
			allowStricter: false,
			want: map[string]bool{
				"require_pull_request":             true,
				"required_approvals":               true,
				"dismiss_stale_reviews":            true,
				"require_code_owner_reviews":       true,
				"require_status_checks":            true,
				"strict_status_checks":             true,
				"required_checks":                  true,
				"enforce_admins":                   true,
				"allow_force_pushes":               true,
				"allow_deletions":                  true,
				"required_linear_history":          true,
				"required_conversation_resolution": true,
			},
		},
		{
			name:          "exact_match_passes_strict_true",
			desired:       fullDesired,
			actual:        fullDesired,
			allowStricter: true,
			want: map[string]bool{
				"require_pull_request": true,
				"required_approvals":   true,
				"required_checks":      true,
				"enforce_admins":       true,
				"allow_force_pushes":   true,
			},
		},
		{
			// true-is-stricter rule: actual stricter (true) than desired (false).
			name:          "stricter_bool_fails_when_not_allowed",
			desired:       Rules{EnforceAdmins: false},
			actual:        Rules{EnforceAdmins: true},
			allowStricter: false,
			want:          map[string]bool{"enforce_admins": false},
		},
		{
			name:          "stricter_bool_passes_when_allowed",
			desired:       Rules{EnforceAdmins: false},
			actual:        Rules{EnforceAdmins: true},
			allowStricter: true,
			want:          map[string]bool{"enforce_admins": true},
		},
		{
			// allow_* rule: false is stricter; actual false vs desired true.
			name:          "stricter_allow_rule_fails_when_not_allowed",
			desired:       Rules{AllowForcePushes: true},
			actual:        Rules{AllowForcePushes: false},
			allowStricter: false,
			want:          map[string]bool{"allow_force_pushes": false},
		},
		{
			name:          "stricter_allow_rule_passes_when_allowed",
			desired:       Rules{AllowForcePushes: true},
			actual:        Rules{AllowForcePushes: false},
			allowStricter: true,
			want:          map[string]bool{"allow_force_pushes": true},
		},
		{
			// required_approvals: actual greater than desired.
			name:          "more_approvals_fails_when_not_allowed",
			desired:       Rules{RequirePullRequest: true, RequiredApprovals: 1},
			actual:        Rules{RequirePullRequest: true, RequiredApprovals: 3},
			allowStricter: false,
			want:          map[string]bool{"required_approvals": false},
		},
		{
			name:          "more_approvals_passes_when_allowed",
			desired:       Rules{RequirePullRequest: true, RequiredApprovals: 1},
			actual:        Rules{RequirePullRequest: true, RequiredApprovals: 3},
			allowStricter: true,
			want:          map[string]bool{"required_approvals": true},
		},
		{
			// required_approvals: actual lower than desired always fails.
			name:          "fewer_approvals_fails_even_when_allowed",
			desired:       Rules{RequirePullRequest: true, RequiredApprovals: 2},
			actual:        Rules{RequirePullRequest: true, RequiredApprovals: 1},
			allowStricter: true,
			want:          map[string]bool{"required_approvals": false},
		},
		{
			// required_checks: actual superset of desired.
			name:          "checks_superset_fails_when_not_allowed",
			desired:       Rules{RequireStatusChecks: true, RequiredChecks: []string{"build"}},
			actual:        Rules{RequireStatusChecks: true, RequiredChecks: []string{"build", "test"}},
			allowStricter: false,
			want:          map[string]bool{"required_checks": false},
		},
		{
			name:          "checks_superset_passes_when_allowed",
			desired:       Rules{RequireStatusChecks: true, RequiredChecks: []string{"build"}},
			actual:        Rules{RequireStatusChecks: true, RequiredChecks: []string{"build", "test"}},
			allowStricter: true,
			want:          map[string]bool{"required_checks": true},
		},
		{
			// required_checks: missing a desired check always fails.
			name:          "checks_missing_fails_even_when_allowed",
			desired:       Rules{RequireStatusChecks: true, RequiredChecks: []string{"build", "test"}},
			actual:        Rules{RequireStatusChecks: true, RequiredChecks: []string{"build"}},
			allowStricter: true,
			want:          map[string]bool{"required_checks": false},
		},
		{
			// Sub-rules are not emitted when their parent toggle is false.
			name:          "pr_disabled_omits_sub_rules",
			desired:       Rules{RequirePullRequest: false, RequiredApprovals: 2},
			actual:        Rules{},
			allowStricter: false,
			want:          map[string]bool{"require_pull_request": true},
			absent:        []string{"required_approvals", "dismiss_stale_reviews", "require_code_owner_reviews"},
		},
		{
			name:          "status_checks_disabled_omits_sub_rules",
			desired:       Rules{RequireStatusChecks: false, RequiredChecks: []string{"build"}},
			actual:        Rules{},
			allowStricter: false,
			want:          map[string]bool{"require_status_checks": true},
			absent:        []string{"strict_status_checks", "required_checks"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := Compare(tt.desired, tt.actual, tt.allowStricter)
			for rule, wantPass := range tt.want {
				d := findDiff(diffs, rule)
				if d == nil {
					t.Fatalf("expected a %q diff, got none", rule)
				}
				if d.Pass != wantPass {
					t.Errorf("rule %q: Pass=%t, want %t (want=%q got=%q)", rule, d.Pass, wantPass, d.Want, d.Got)
				}
			}
			for _, rule := range tt.absent {
				if d := findDiff(diffs, rule); d != nil {
					t.Errorf("expected no %q diff, got %+v", rule, d)
				}
			}
		})
	}
}

// writeTempConfig writes content to a temp config file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dPath := filepath.Join(t.TempDir(), "rampart.yaml")
	if err := os.WriteFile(dPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dPath
}

func TestLoad_RejectsUnknownFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
		// wantErr is a substring the error must name so the operator can find
		// the offending key.
		wantErr string
	}{
		{
			name: "top_level",
			content: `
branch: main
allow_stricter_rule: true
`,
			wantErr: "allow_stricter_rule",
		},
		{
			name: "base_rule",
			content: `
rules:
  require_pull_request: true
  enforce_admin: true
`,
			wantErr: "enforce_admin",
		},
		{
			name: "base_restriction",
			content: `
rules:
  restrictions:
    users: [wdm0006]
    team: [platform]
`,
			wantErr: "team",
		},
		{
			name: "override",
			content: `
overrides:
  - repo: ["prod-*"]
    rules:
      required_approvals: 3
`,
			wantErr: "repo",
		},
		{
			name: "override_rule",
			content: `
overrides:
  - repos: ["prod-*"]
    rules:
      enforce_admins: true
      dismiss_stale_review: true
`,
			wantErr: "dismiss_stale_review",
		},
		{
			name: "override_restriction",
			content: `
overrides:
  - repos: ["prod-*"]
    rules:
      restrictions:
        users: [wdm0006]
        app: [renovate]
`,
			wantErr: "app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, tt.content))
			if err == nil {
				t.Fatal("expected Load to reject the unknown field, got nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name the unknown field %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_RejectsInvalidOverridePattern(t *testing.T) {
	patterns := []string{"[", "prod-[a-", "infra-*[", `bad\`}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			content := fmt.Sprintf(`
rules:
  require_pull_request: true
overrides:
  - repos: ["ok-*", %q]
    rules:
      required_approvals: 3
`, pattern)
			_, err := Load(writeTempConfig(t, content))
			if err == nil {
				t.Fatal("expected Load to reject the malformed glob, got nil error")
			}
			if !strings.Contains(err.Error(), pattern) {
				t.Errorf("error %q does not name the invalid pattern %q", err, pattern)
			}
		})
	}
}

func TestLoad_ValidConfigStillLoads(t *testing.T) {
	content := `
branch: main
allow_stricter_rules: true
rules:
  require_pull_request: true
  required_approvals: 2
  dismiss_stale_reviews: true
  require_code_owner_reviews: false
  require_status_checks: true
  strict_status_checks: true
  required_checks: [build, test]
  enforce_admins: true
  allow_force_pushes: false
  allow_deletions: false
  required_linear_history: false
  required_conversation_resolution: true
  restrictions:
    users: [wdm0006]
    teams: []
    apps: [renovate]
overrides:
  - repos: ["docs-*", "sandbox-?"]
    rules:
      required_approvals: 0
      required_checks: []
      restrictions: null
`
	cfg, err := Load(writeTempConfig(t, content))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Branch != "main" || !cfg.AllowStricterRules {
		t.Errorf("unexpected top level: branch=%q allow_stricter_rules=%t", cfg.Branch, cfg.AllowStricterRules)
	}
	if !reflect.DeepEqual(cfg.Rules.RequiredChecks, []string{"build", "test"}) {
		t.Errorf("base required_checks=%v, want [build test]", cfg.Rules.RequiredChecks)
	}
	if cfg.Rules.Restrictions == nil || !reflect.DeepEqual(cfg.Rules.Restrictions.Apps, []string{"renovate"}) {
		t.Errorf("base restrictions=%+v, want apps=[renovate]", cfg.Rules.Restrictions)
	}

	base := cfg.RulesForRepo("api")
	if base.RequiredApprovals != 2 || !base.RequireStatusChecks || !base.RequiredConversationResolution {
		t.Errorf("unexpected base rules for api: %+v", base)
	}

	// Explicit empty/null override fields keep their clearing semantics.
	for _, repo := range []string{"docs-site", "sandbox-a"} {
		rules := cfg.RulesForRepo(repo)
		if rules.RequiredApprovals != 0 {
			t.Errorf("%s: required_approvals=%d, want 0", repo, rules.RequiredApprovals)
		}
		if len(rules.RequiredChecks) != 0 {
			t.Errorf("%s: required_checks=%v, want empty", repo, rules.RequiredChecks)
		}
		if rules.Restrictions != nil {
			t.Errorf("%s: restrictions=%+v, want nil", repo, rules.Restrictions)
		}
		if !rules.RequireStatusChecks {
			t.Errorf("%s: expected require_status_checks inherited from base", repo)
		}
	}

	// A repo matching no override keeps the base allowlist.
	if r := cfg.RulesForRepo("sandbox-long-name"); r.Restrictions == nil {
		t.Error("expected sandbox-long-name to keep the base restrictions")
	}
}

func TestLoad_EmptyFileUsesDefaults(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, ""))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Branch != "default" {
		t.Errorf("branch=%q, want default", cfg.Branch)
	}
	if cfg.Rules.RequiredChecks == nil {
		t.Error("expected required_checks to be non-nil")
	}
}
