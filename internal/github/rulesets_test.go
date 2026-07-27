package github

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

// pr builds a pull_request branchRule with the given parameter blob.
func pr(t *testing.T, params pullRequestParams) branchRule {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("failed to marshal pull request params: %v", err)
	}
	return branchRule{Type: "pull_request", Parameters: raw}
}

// checks builds a required_status_checks branchRule from contexts.
func checks(t *testing.T, strict bool, contexts ...string) branchRule {
	t.Helper()
	var params statusCheckParams
	params.StrictRequiredStatusChecksPolicy = strict
	for _, c := range contexts {
		params.RequiredStatusChecks = append(params.RequiredStatusChecks, struct {
			Context string `json:"context"`
		}{Context: c})
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("failed to marshal status check params: %v", err)
	}
	return branchRule{Type: "required_status_checks", Parameters: raw}
}

func TestClassifyBranchRulesError(t *testing.T) {
	tests := []struct {
		name    string
		stderr  string
		handled bool
		wantErr string
	}{
		{name: "404 status", stderr: "gh: rules unavailable (HTTP 404)", handled: true},
		{name: "not found wording", stderr: "gh: Not Found", handled: true},
		// Ruleset reads intentionally swallow 403 errors, unlike classic protection reads.
		{name: "403 status", stderr: "gh: Resource not accessible (HTTP 403)", handled: true},
		{name: "unrecognized error", stderr: "gh: service unavailable", wantErr: "gh api failed: gh: service unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handled, err := classifyBranchRulesError(tt.stderr)
			if handled != tt.handled {
				t.Errorf("classifyBranchRulesError() handled = %v, want %v", handled, tt.handled)
			}
			if gotErr := errorString(err); gotErr != tt.wantErr {
				t.Errorf("classifyBranchRulesError() error = %q, want %q", gotErr, tt.wantErr)
			}
		})
	}
}

func TestClassifyRulesetListError(t *testing.T) {
	tests := []struct {
		name    string
		stderr  string
		handled bool
		wantErr string
	}{
		{name: "404 status", stderr: "gh: rulesets unavailable (HTTP 404)", handled: true},
		{name: "not found wording", stderr: "gh: Not Found", handled: true},
		{name: "unrecognized error", stderr: "gh: service unavailable", wantErr: "failed to list rulesets: gh: service unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handled, err := classifyRulesetListError(tt.stderr)
			if handled != tt.handled {
				t.Errorf("classifyRulesetListError() handled = %v, want %v", handled, tt.handled)
			}
			if gotErr := errorString(err); gotErr != tt.wantErr {
				t.Errorf("classifyRulesetListError() error = %q, want %q", gotErr, tt.wantErr)
			}
		})
	}
}

func TestRulesFromBranchRules(t *testing.T) {
	tests := []struct {
		name  string
		input []branchRule
		want  config.Rules
	}{
		{
			name:  "empty input yields permissive defaults",
			input: nil,
			want: config.Rules{
				RequiredChecks:   []string{},
				AllowForcePushes: true,
				AllowDeletions:   true,
			},
		},
		{
			name: "pull_request with parameters maps fields",
			input: []branchRule{
				pr(t, pullRequestParams{
					RequiredApprovingReviewCount:   2,
					DismissStaleReviewsOnPush:      true,
					RequireCodeOwnerReview:         true,
					RequiredReviewThreadResolution: true,
				}),
			},
			want: config.Rules{
				RequirePullRequest:             true,
				RequiredApprovals:              2,
				DismissStaleReviews:            true,
				RequireCodeOwnerReviews:        true,
				RequiredConversationResolution: true,
				RequiredChecks:                 []string{},
				AllowForcePushes:               true,
				AllowDeletions:                 true,
			},
		},
		{
			name: "pull_request without parameters only sets RequirePullRequest",
			input: []branchRule{
				{Type: "pull_request"},
			},
			want: config.Rules{
				RequirePullRequest: true,
				RequiredChecks:     []string{},
				AllowForcePushes:   true,
				AllowDeletions:     true,
			},
		},
		{
			name: "two pull_request rules take higher approvals and OR flags",
			input: []branchRule{
				pr(t, pullRequestParams{
					RequiredApprovingReviewCount: 1,
					DismissStaleReviewsOnPush:    true,
					RequireCodeOwnerReview:       false,
				}),
				pr(t, pullRequestParams{
					RequiredApprovingReviewCount:   3,
					DismissStaleReviewsOnPush:      false,
					RequireCodeOwnerReview:         true,
					RequiredReviewThreadResolution: true,
				}),
			},
			want: config.Rules{
				RequirePullRequest:             true,
				RequiredApprovals:              3,
				DismissStaleReviews:            true,
				RequireCodeOwnerReviews:        true,
				RequiredConversationResolution: true,
				RequiredChecks:                 []string{},
				AllowForcePushes:               true,
				AllowDeletions:                 true,
			},
		},
		{
			name: "required_status_checks from two rulesets merge and dedup",
			input: []branchRule{
				checks(t, false, "build", "test"),
				checks(t, true, "test", "lint"),
			},
			want: config.Rules{
				RequireStatusChecks: true,
				StrictStatusChecks:  true,
				RequiredChecks:      []string{"build", "test", "lint"},
				AllowForcePushes:    true,
				AllowDeletions:      true,
			},
		},
		{
			name: "non_fast_forward deletion and required_linear_history flip fields",
			input: []branchRule{
				{Type: "non_fast_forward"},
				{Type: "deletion"},
				{Type: "required_linear_history"},
			},
			want: config.Rules{
				RequiredChecks:        []string{},
				AllowForcePushes:      false,
				AllowDeletions:        false,
				RequiredLinearHistory: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rulesFromBranchRules(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rulesFromBranchRules() mismatch\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestBuildRulesetPayload_EnforceAdmins(t *testing.T) {
	t.Run("enforce_admins=false adds RepositoryRole bypass actor", func(t *testing.T) {
		payload := buildRulesetPayload("main", config.Rules{EnforceAdmins: false})
		actors, ok := payload["bypass_actors"].([]interface{})
		if !ok {
			t.Fatalf("bypass_actors not a slice: %T", payload["bypass_actors"])
		}
		if len(actors) != 1 {
			t.Fatalf("expected 1 bypass actor, got %d", len(actors))
		}
		actor, ok := actors[0].(map[string]interface{})
		if !ok {
			t.Fatalf("bypass actor not a map: %T", actors[0])
		}
		if actor["actor_id"] != 5 {
			t.Errorf("expected actor_id=5, got %v", actor["actor_id"])
		}
		if actor["actor_type"] != "RepositoryRole" {
			t.Errorf("expected actor_type=RepositoryRole, got %v", actor["actor_type"])
		}
	})

	t.Run("enforce_admins=true yields empty bypass_actors", func(t *testing.T) {
		payload := buildRulesetPayload("main", config.Rules{EnforceAdmins: true})
		actors, ok := payload["bypass_actors"].([]interface{})
		if !ok {
			t.Fatalf("bypass_actors not a slice: %T", payload["bypass_actors"])
		}
		if len(actors) != 0 {
			t.Errorf("expected empty bypass_actors, got %d entries", len(actors))
		}
	})
}

func TestEnforceAdminsFromBypassActors(t *testing.T) {
	tests := []struct {
		name   string
		actors []bypassActor
		want   bool
	}{
		{name: "no bypass actors enforces admins", want: true},
		{
			name: "admin repository role bypass does not enforce admins",
			actors: []bypassActor{
				{ActorID: 5, ActorType: "RepositoryRole"},
			},
			want: false,
		},
		{
			name: "other repository role does not bypass admins",
			actors: []bypassActor{
				{ActorID: 4, ActorType: "RepositoryRole"},
			},
			want: true,
		},
		{
			name: "same actor ID with another type does not bypass admins",
			actors: []bypassActor{
				{ActorID: 5, ActorType: "Team"},
			},
			want: true,
		},
		{
			name: "admin bypass among other actors does not enforce admins",
			actors: []bypassActor{
				{ActorID: 1, ActorType: "OrganizationAdmin"},
				{ActorID: 5, ActorType: "RepositoryRole"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := enforceAdminsFromBypassActors(tt.actors); got != tt.want {
				t.Errorf("enforceAdminsFromBypassActors() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildRulesetPayload_Conditions(t *testing.T) {
	payload := buildRulesetPayload("develop", config.Rules{})

	if payload["name"] != "rampart" {
		t.Errorf("expected name=rampart, got %v", payload["name"])
	}

	conditions, ok := payload["conditions"].(map[string]interface{})
	if !ok {
		t.Fatalf("conditions not a map: %T", payload["conditions"])
	}
	refName, ok := conditions["ref_name"].(map[string]interface{})
	if !ok {
		t.Fatalf("ref_name not a map: %T", conditions["ref_name"])
	}
	include, ok := refName["include"].([]string)
	if !ok {
		t.Fatalf("include not a []string: %T", refName["include"])
	}
	want := []string{"refs/heads/develop"}
	if !reflect.DeepEqual(include, want) {
		t.Errorf("expected include=%v, got %v", want, include)
	}
}

func TestBuildRulesetPayload_RuleEntries(t *testing.T) {
	// ruleTypes extracts the set of rule "type" values from a payload.
	ruleTypes := func(payload map[string]interface{}) map[string]bool {
		rules, ok := payload["rules"].([]interface{})
		if !ok && payload["rules"] != nil {
			t.Fatalf("rules not a slice: %T", payload["rules"])
		}
		types := make(map[string]bool)
		for _, r := range rules {
			m, ok := r.(map[string]interface{})
			if !ok {
				t.Fatalf("rule not a map: %T", r)
			}
			types[m["type"].(string)] = true
		}
		return types
	}

	t.Run("all rules enabled includes every entry", func(t *testing.T) {
		rules := config.Rules{
			RequirePullRequest:    true,
			RequireStatusChecks:   true,
			RequiredChecks:        []string{"build"},
			AllowForcePushes:      false,
			AllowDeletions:        false,
			RequiredLinearHistory: true,
		}
		types := ruleTypes(buildRulesetPayload("main", rules))
		for _, want := range []string{"pull_request", "required_status_checks", "non_fast_forward", "deletion", "required_linear_history"} {
			if !types[want] {
				t.Errorf("expected rule entry %q to be present", want)
			}
		}
	})

	t.Run("permissive rules omit conditional entries", func(t *testing.T) {
		// AllowForcePushes/AllowDeletions true => no non_fast_forward/deletion entries.
		rules := config.Rules{
			AllowForcePushes: true,
			AllowDeletions:   true,
		}
		types := ruleTypes(buildRulesetPayload("main", rules))
		for _, absent := range []string{"pull_request", "required_status_checks", "non_fast_forward", "deletion", "required_linear_history"} {
			if types[absent] {
				t.Errorf("expected rule entry %q to be absent", absent)
			}
		}
	})

	t.Run("status checks parameters carry contexts", func(t *testing.T) {
		rules := config.Rules{
			RequireStatusChecks: true,
			StrictStatusChecks:  true,
			RequiredChecks:      []string{"build", "test"},
		}
		payload := buildRulesetPayload("main", rules)
		entries := payload["rules"].([]interface{})
		var found bool
		for _, e := range entries {
			m := e.(map[string]interface{})
			if m["type"] != "required_status_checks" {
				continue
			}
			found = true
			params := m["parameters"].(map[string]interface{})
			if params["strict_required_status_checks_policy"] != true {
				t.Errorf("expected strict policy true, got %v", params["strict_required_status_checks_policy"])
			}
			checks := params["required_status_checks"].([]map[string]interface{})
			if len(checks) != 2 {
				t.Fatalf("expected 2 checks, got %d", len(checks))
			}
			if checks[0]["context"] != "build" || checks[1]["context"] != "test" {
				t.Errorf("unexpected check contexts: %v", checks)
			}
		}
		if !found {
			t.Error("required_status_checks entry not found")
		}
	})
}

func TestDedup(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", []string{}, []string{}},
		{"no duplicates preserves order", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"removes duplicates keeping first-seen order", []string{"b", "a", "b", "c", "a"}, []string{"b", "a", "c"}},
		{"all duplicates", []string{"x", "x", "x"}, []string{"x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedup(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dedup(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRampartRulesetID(t *testing.T) {
	tests := []struct {
		name     string
		rulesets []rulesetListEntry
		want     int
	}{
		{
			name: "finds rampart after other rulesets",
			rulesets: []rulesetListEntry{
				{ID: 10, Name: "default"},
				{ID: 20, Name: "release"},
				{ID: 30, Name: "rampart"},
			},
			want: 30,
		},
		{
			name: "returns zero when rampart is absent",
			rulesets: []rulesetListEntry{
				{ID: 10, Name: "default"},
				{ID: 20, Name: "release"},
			},
			want: 0,
		},
		{
			name: "returns first rampart when names are duplicated",
			rulesets: []rulesetListEntry{
				{ID: 10, Name: "rampart"},
				{ID: 20, Name: "default"},
				{ID: 30, Name: "rampart"},
			},
			want: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rampartRulesetID(tt.rulesets); got != tt.want {
				t.Errorf("rampartRulesetID() = %d, want %d", got, tt.want)
			}
		})
	}
}
