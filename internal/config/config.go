package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the rampart configuration file
type Config struct {
	Branch string `yaml:"branch"`
	Rules  Rules  `yaml:"rules"`
}

// Rules represents the desired branch protection rules
type Rules struct {
	RequirePullRequest             bool     `yaml:"require_pull_request"`
	RequiredApprovals              int      `yaml:"required_approvals"`
	DismissStaleReviews            bool     `yaml:"dismiss_stale_reviews"`
	RequireCodeOwnerReviews        bool     `yaml:"require_code_owner_reviews"`
	RequireStatusChecks            bool     `yaml:"require_status_checks"`
	StrictStatusChecks             bool     `yaml:"strict_status_checks"`
	RequiredChecks                 []string `yaml:"required_checks"`
	EnforceAdmins                  bool     `yaml:"enforce_admins"`
	AllowForcePushes               bool     `yaml:"allow_force_pushes"`
	AllowDeletions                 bool     `yaml:"allow_deletions"`
	RequiredLinearHistory          bool     `yaml:"required_linear_history"`
	RequiredConversationResolution bool     `yaml:"required_conversation_resolution"`
}

// RuleDiff represents a single rule comparison result
type RuleDiff struct {
	Rule string
	Pass bool
	Want string
	Got  string
}

// Default returns a Config with sensible defaults
func Default() Config {
	return Config{
		Branch: "main",
		Rules: Rules{
			RequirePullRequest:             true,
			RequiredApprovals:              1,
			DismissStaleReviews:            true,
			RequireCodeOwnerReviews:        false,
			RequireStatusChecks:            false,
			StrictStatusChecks:             true,
			RequiredChecks:                 []string{},
			EnforceAdmins:                  true,
			AllowForcePushes:               false,
			AllowDeletions:                 false,
			RequiredLinearHistory:          false,
			RequiredConversationResolution: false,
		},
	}
}

// Load reads and parses a rampart config file
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to parse config: %w", err)
	}

	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	if cfg.Rules.RequiredChecks == nil {
		cfg.Rules.RequiredChecks = []string{}
	}

	return cfg, nil
}

// WriteDefault writes the default config to a file
func WriteDefault(path string) error {
	cfg := Default()
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// ToAPIPayload translates Rules into the GitHub API PUT payload for branch protection
func (r Rules) ToAPIPayload() map[string]interface{} {
	payload := map[string]interface{}{
		"enforce_admins":                   r.EnforceAdmins,
		"allow_force_pushes":               r.AllowForcePushes,
		"allow_deletions":                  r.AllowDeletions,
		"required_linear_history":          r.RequiredLinearHistory,
		"required_conversation_resolution": r.RequiredConversationResolution,
		"restrictions":                     nil,
	}

	if r.RequirePullRequest {
		reviews := map[string]interface{}{
			"required_approving_review_count": r.RequiredApprovals,
			"dismiss_stale_reviews":           r.DismissStaleReviews,
			"require_code_owner_reviews":      r.RequireCodeOwnerReviews,
		}
		payload["required_pull_request_reviews"] = reviews
	} else {
		payload["required_pull_request_reviews"] = nil
	}

	if r.RequireStatusChecks {
		checks := make([]map[string]string, len(r.RequiredChecks))
		for i, c := range r.RequiredChecks {
			checks[i] = map[string]string{"context": c}
		}
		payload["required_status_checks"] = map[string]interface{}{
			"strict":   r.StrictStatusChecks,
			"contexts": r.RequiredChecks,
			"checks":   checks,
		}
	} else {
		payload["required_status_checks"] = nil
	}

	return payload
}

// ProtectionResponse represents the GitHub API response for branch protection
type ProtectionResponse struct {
	RequiredPullRequestReviews *struct {
		RequiredApprovingReviewCount int  `json:"required_approving_review_count"`
		DismissStaleReviews          bool `json:"dismiss_stale_reviews"`
		RequireCodeOwnerReviews      bool `json:"require_code_owner_reviews"`
	} `json:"required_pull_request_reviews"`
	RequiredStatusChecks *struct {
		Strict   bool     `json:"strict"`
		Contexts []string `json:"contexts"`
	} `json:"required_status_checks"`
	EnforceAdmins struct {
		Enabled bool `json:"enabled"`
	} `json:"enforce_admins"`
	AllowForcePushes struct {
		Enabled bool `json:"enabled"`
	} `json:"allow_force_pushes"`
	AllowDeletions struct {
		Enabled bool `json:"enabled"`
	} `json:"allow_deletions"`
	RequiredLinearHistory struct {
		Enabled bool `json:"enabled"`
	} `json:"required_linear_history"`
	RequiredConversationResolution struct {
		Enabled bool `json:"enabled"`
	} `json:"required_conversation_resolution"`
}

// RulesFromResponse converts a GitHub API protection response into Rules
func RulesFromResponse(resp ProtectionResponse) Rules {
	r := Rules{
		EnforceAdmins:                  resp.EnforceAdmins.Enabled,
		AllowForcePushes:               resp.AllowForcePushes.Enabled,
		AllowDeletions:                 resp.AllowDeletions.Enabled,
		RequiredLinearHistory:          resp.RequiredLinearHistory.Enabled,
		RequiredConversationResolution: resp.RequiredConversationResolution.Enabled,
		RequiredChecks:                 []string{},
	}

	if resp.RequiredPullRequestReviews != nil {
		r.RequirePullRequest = true
		r.RequiredApprovals = resp.RequiredPullRequestReviews.RequiredApprovingReviewCount
		r.DismissStaleReviews = resp.RequiredPullRequestReviews.DismissStaleReviews
		r.RequireCodeOwnerReviews = resp.RequiredPullRequestReviews.RequireCodeOwnerReviews
	}

	if resp.RequiredStatusChecks != nil {
		r.RequireStatusChecks = true
		r.StrictStatusChecks = resp.RequiredStatusChecks.Strict
		if resp.RequiredStatusChecks.Contexts != nil {
			r.RequiredChecks = resp.RequiredStatusChecks.Contexts
		}
	}

	return r
}

// Compare compares desired rules against actual rules and returns diffs
func Compare(desired, actual Rules) []RuleDiff {
	var diffs []RuleDiff

	addDiff := func(rule string, pass bool, want, got string) {
		diffs = append(diffs, RuleDiff{Rule: rule, Pass: pass, Want: want, Got: got})
	}

	addBoolDiff := func(rule string, want, got bool) {
		addDiff(rule, want == got, fmt.Sprintf("%t", want), fmt.Sprintf("%t", got))
	}

	// Pull request reviews
	addBoolDiff("require_pull_request", desired.RequirePullRequest, actual.RequirePullRequest)
	if desired.RequirePullRequest {
		approvalPass := desired.RequiredApprovals == actual.RequiredApprovals
		addDiff("required_approvals", approvalPass,
			fmt.Sprintf("%d", desired.RequiredApprovals),
			fmt.Sprintf("%d", actual.RequiredApprovals))
		addBoolDiff("dismiss_stale_reviews", desired.DismissStaleReviews, actual.DismissStaleReviews)
		addBoolDiff("require_code_owner_reviews", desired.RequireCodeOwnerReviews, actual.RequireCodeOwnerReviews)
	}

	// Status checks
	addBoolDiff("require_status_checks", desired.RequireStatusChecks, actual.RequireStatusChecks)
	if desired.RequireStatusChecks {
		addBoolDiff("strict_status_checks", desired.StrictStatusChecks, actual.StrictStatusChecks)
		// Compare required checks
		checksMatch := len(desired.RequiredChecks) == len(actual.RequiredChecks)
		if checksMatch {
			desiredSet := make(map[string]bool)
			for _, c := range desired.RequiredChecks {
				desiredSet[c] = true
			}
			for _, c := range actual.RequiredChecks {
				if !desiredSet[c] {
					checksMatch = false
					break
				}
			}
		}
		addDiff("required_checks", checksMatch,
			fmt.Sprintf("%v", desired.RequiredChecks),
			fmt.Sprintf("%v", actual.RequiredChecks))
	}

	// Other rules
	addBoolDiff("enforce_admins", desired.EnforceAdmins, actual.EnforceAdmins)
	addBoolDiff("allow_force_pushes", desired.AllowForcePushes, actual.AllowForcePushes)
	addBoolDiff("allow_deletions", desired.AllowDeletions, actual.AllowDeletions)
	addBoolDiff("required_linear_history", desired.RequiredLinearHistory, actual.RequiredLinearHistory)
	addBoolDiff("required_conversation_resolution", desired.RequiredConversationResolution, actual.RequiredConversationResolution)

	return diffs
}
