package github

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wdm0006/rampart/internal/config"
)

// branchRule represents a single rule from the effective branch rules endpoint.
type branchRule struct {
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	RulesetID  int             `json:"ruleset_id"`
}

type bypassActor struct {
	ActorID   int    `json:"actor_id"`
	ActorType string `json:"actor_type"`
}

type rulesetDetail struct {
	BypassActors []bypassActor `json:"bypass_actors"`
}

type pullRequestParams struct {
	RequiredApprovingReviewCount   int  `json:"required_approving_review_count"`
	DismissStaleReviewsOnPush      bool `json:"dismiss_stale_reviews_on_push"`
	RequireCodeOwnerReview         bool `json:"require_code_owner_review"`
	RequiredReviewThreadResolution bool `json:"required_review_thread_resolution"`
}

type statusCheckParams struct {
	StrictRequiredStatusChecksPolicy bool `json:"strict_required_status_checks_policy"`
	RequiredStatusChecks             []struct {
		Context string `json:"context"`
	} `json:"required_status_checks"`
}

// rulesetListEntry represents a ruleset from the list endpoint.
type rulesetListEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetBranchRules returns the effective ruleset-based rules for a branch
// by querying the GitHub branch rules endpoint. Returns permissive defaults
// if no rulesets apply or the endpoint is unavailable.
func GetBranchRules(owner, repo, branch string) (config.Rules, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/rules/branches/%s", owner, repo, branch)
	cmd := exec.Command("gh", "api", endpoint, "--paginate")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			handled, classifyErr := classifyBranchRulesError(string(exitErr.Stderr))
			if handled {
				return config.NoProtectionRules(), nil
			}
			return config.Rules{}, classifyErr
		}
		return config.Rules{}, fmt.Errorf("failed to run gh: %w", err)
	}

	var rules []branchRule
	if err := json.Unmarshal(output, &rules); err != nil {
		return config.Rules{}, fmt.Errorf("failed to parse branch rules: %w", err)
	}

	if len(rules) == 0 {
		return config.NoProtectionRules(), nil
	}

	r := rulesFromBranchRules(rules)
	rulesetIDs := make([]int, 0)
	for _, rule := range rules {
		if rule.RulesetID == 0 {
			return r, nil
		}
		rulesetIDs = append(rulesetIDs, rule.RulesetID)
	}

	for _, rulesetID := range dedupInts(rulesetIDs) {
		detail, err := getRulesetDetail(owner, repo, rulesetID)
		if err != nil {
			return r, nil
		}
		if !enforceAdminsFromBypassActors(detail.BypassActors) {
			return r, nil
		}
	}
	r.EnforceAdmins = true

	return r, nil
}

func classifyBranchRulesError(stderr string) (bool, error) {
	if strings.Contains(stderr, "404") || strings.Contains(stderr, "Not Found") ||
		strings.Contains(stderr, "403") {
		return true, nil
	}
	return false, fmt.Errorf("gh api failed: %s", stderr)
}

func getRulesetDetail(owner, repo string, rulesetID int) (rulesetDetail, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/rulesets/%d", owner, repo, rulesetID)
	output, err := exec.Command("gh", "api", endpoint).Output()
	if err != nil {
		return rulesetDetail{}, err
	}

	var detail rulesetDetail
	if err := json.Unmarshal(output, &detail); err != nil {
		return rulesetDetail{}, err
	}
	return detail, nil
}

func enforceAdminsFromBypassActors(actors []bypassActor) bool {
	for _, actor := range actors {
		if actor.ActorType == "RepositoryRole" && actor.ActorID == 5 {
			return false
		}
	}
	return true
}

// rulesFromBranchRules converts the effective branch rules API response into
// a Rules struct. When multiple rules of the same type exist (from different
// rulesets), the most restrictive value is used for each field.
func rulesFromBranchRules(branchRules []branchRule) config.Rules {
	r := config.Rules{
		RequiredChecks:   []string{},
		AllowForcePushes: true,
		AllowDeletions:   true,
	}

	for _, rule := range branchRules {
		switch rule.Type {
		case "pull_request":
			r.RequirePullRequest = true
			if rule.Parameters != nil {
				var params pullRequestParams
				if err := json.Unmarshal(rule.Parameters, &params); err == nil {
					if params.RequiredApprovingReviewCount > r.RequiredApprovals {
						r.RequiredApprovals = params.RequiredApprovingReviewCount
					}
					r.DismissStaleReviews = r.DismissStaleReviews || params.DismissStaleReviewsOnPush
					r.RequireCodeOwnerReviews = r.RequireCodeOwnerReviews || params.RequireCodeOwnerReview
					r.RequiredConversationResolution = r.RequiredConversationResolution || params.RequiredReviewThreadResolution
				}
			}
		case "required_status_checks":
			r.RequireStatusChecks = true
			if rule.Parameters != nil {
				var params statusCheckParams
				if err := json.Unmarshal(rule.Parameters, &params); err == nil {
					r.StrictStatusChecks = r.StrictStatusChecks || params.StrictRequiredStatusChecksPolicy
					for _, check := range params.RequiredStatusChecks {
						r.RequiredChecks = append(r.RequiredChecks, check.Context)
					}
				}
			}
		case "non_fast_forward":
			r.AllowForcePushes = false
		case "deletion":
			r.AllowDeletions = false
		case "required_linear_history":
			r.RequiredLinearHistory = true
		}
	}

	// Deduplicate required checks
	r.RequiredChecks = dedup(r.RequiredChecks)

	return r
}

func dedup(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func dedupInts(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// SetRuleset creates or updates a "rampart" ruleset for the given branch.
// If a ruleset named "rampart" already exists, it is updated. Otherwise a
// new one is created.
func SetRuleset(owner, repo, branch string, rules config.Rules) error {
	rulesetID, err := findRampartRuleset(owner, repo)
	if err != nil {
		return err
	}

	payload := buildRulesetPayload(branch, rules)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal ruleset payload: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "rampart-ruleset-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(payloadJSON); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	var endpoint, method string
	if rulesetID > 0 {
		endpoint = fmt.Sprintf("repos/%s/%s/rulesets/%d", owner, repo, rulesetID)
		method = "PUT"
	} else {
		endpoint = fmt.Sprintf("repos/%s/%s/rulesets", owner, repo)
		method = "POST"
	}

	cmd := exec.Command("gh", "api", endpoint,
		"--method", method,
		"--input", tmpFile.Name(),
		"-H", "Accept: application/vnd.github+json",
	)
	_, err = cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("failed to set ruleset: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to run gh: %w", err)
	}

	return nil
}

func findRampartRuleset(owner, repo string) (int, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/rulesets", owner, repo)
	cmd := exec.Command("gh", "api", endpoint, "--paginate")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			handled, classifyErr := classifyRulesetListError(string(exitErr.Stderr))
			if handled {
				return 0, nil
			}
			return 0, classifyErr
		}
		return 0, fmt.Errorf("failed to run gh: %w", err)
	}

	var rulesets []rulesetListEntry
	if err := json.Unmarshal(output, &rulesets); err != nil {
		return 0, fmt.Errorf("failed to parse rulesets: %w", err)
	}

	return rampartRulesetID(rulesets), nil
}

func classifyRulesetListError(stderr string) (bool, error) {
	if strings.Contains(stderr, "404") || strings.Contains(stderr, "Not Found") {
		return true, nil
	}
	return false, fmt.Errorf("failed to list rulesets: %s", stderr)
}

func rampartRulesetID(rulesets []rulesetListEntry) int {
	for _, ruleset := range rulesets {
		if ruleset.Name == "rampart" {
			return ruleset.ID
		}
	}
	return 0
}

func buildRulesetPayload(branch string, rules config.Rules) map[string]interface{} {
	var rulesetRules []interface{}

	if rules.RequirePullRequest {
		rulesetRules = append(rulesetRules, map[string]interface{}{
			"type": "pull_request",
			"parameters": map[string]interface{}{
				"required_approving_review_count":   rules.RequiredApprovals,
				"dismiss_stale_reviews_on_push":     rules.DismissStaleReviews,
				"require_code_owner_review":         rules.RequireCodeOwnerReviews,
				"require_last_push_approval":        false,
				"required_review_thread_resolution": rules.RequiredConversationResolution,
			},
		})
	}

	if rules.RequireStatusChecks {
		checks := make([]map[string]interface{}, len(rules.RequiredChecks))
		for i, c := range rules.RequiredChecks {
			checks[i] = map[string]interface{}{"context": c}
		}
		rulesetRules = append(rulesetRules, map[string]interface{}{
			"type": "required_status_checks",
			"parameters": map[string]interface{}{
				"strict_required_status_checks_policy": rules.StrictStatusChecks,
				"required_status_checks":               checks,
			},
		})
	}

	if !rules.AllowForcePushes {
		rulesetRules = append(rulesetRules, map[string]interface{}{
			"type": "non_fast_forward",
		})
	}

	if !rules.AllowDeletions {
		rulesetRules = append(rulesetRules, map[string]interface{}{
			"type": "deletion",
		})
	}

	if rules.RequiredLinearHistory {
		rulesetRules = append(rulesetRules, map[string]interface{}{
			"type": "required_linear_history",
		})
	}

	// Handle enforce_admins via bypass_actors.
	// RepositoryRole actor_id 5 = Admin role.
	var bypassActors []interface{}
	if !rules.EnforceAdmins {
		bypassActors = []interface{}{
			map[string]interface{}{
				"actor_id":    5,
				"actor_type":  "RepositoryRole",
				"bypass_mode": "always",
			},
		}
	} else {
		bypassActors = []interface{}{}
	}

	return map[string]interface{}{
		"name":        "rampart",
		"target":      "branch",
		"enforcement": "active",
		"conditions": map[string]interface{}{
			"ref_name": map[string]interface{}{
				"include": []string{fmt.Sprintf("refs/heads/%s", branch)},
				"exclude": []string{},
			},
		},
		"bypass_actors": bypassActors,
		"rules":         rulesetRules,
	}
}
