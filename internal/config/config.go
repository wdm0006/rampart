package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the rampart configuration file
type Config struct {
	Branch             string     `yaml:"branch"`
	AllowStricterRules bool       `yaml:"allow_stricter_rules"`
	Rules              Rules      `yaml:"rules"`
	Overrides          []Override `yaml:"overrides"`
}

// Override defines rule overrides for repos matching the given patterns.
// Patterns support exact names and globs (e.g. "prod-*", "*-critical").
type Override struct {
	Repos []string      `yaml:"repos"`
	Rules OverrideRules `yaml:"rules"`
}

// OverrideRules is a sparse version of Rules where all fields are pointers.
// Only non-nil fields override the base config.
type OverrideRules struct {
	RequirePullRequest             *bool         `yaml:"require_pull_request"`
	RequiredApprovals              *int          `yaml:"required_approvals"`
	DismissStaleReviews            *bool         `yaml:"dismiss_stale_reviews"`
	RequireCodeOwnerReviews        *bool         `yaml:"require_code_owner_reviews"`
	RequireStatusChecks            *bool         `yaml:"require_status_checks"`
	StrictStatusChecks             *bool         `yaml:"strict_status_checks"`
	RequiredChecks                 []string      `yaml:"required_checks"`
	EnforceAdmins                  *bool         `yaml:"enforce_admins"`
	AllowForcePushes               *bool         `yaml:"allow_force_pushes"`
	AllowDeletions                 *bool         `yaml:"allow_deletions"`
	RequiredLinearHistory          *bool         `yaml:"required_linear_history"`
	RequiredConversationResolution *bool         `yaml:"required_conversation_resolution"`
	Restrictions                   *Restrictions `yaml:"restrictions"`
	// checksSet tracks whether required_checks was explicitly set in YAML
	// (to distinguish [] from not specified)
	checksSet bool
	// restrictionsSet tracks whether restrictions was explicitly set in YAML
	// (so an override can clear a base allowlist by setting `restrictions: null`).
	restrictionsSet bool
}

// Rules represents the desired branch protection rules
type Rules struct {
	RequirePullRequest             bool          `yaml:"require_pull_request"`
	RequiredApprovals              int           `yaml:"required_approvals"`
	DismissStaleReviews            bool          `yaml:"dismiss_stale_reviews"`
	RequireCodeOwnerReviews        bool          `yaml:"require_code_owner_reviews"`
	RequireStatusChecks            bool          `yaml:"require_status_checks"`
	StrictStatusChecks             bool          `yaml:"strict_status_checks"`
	RequiredChecks                 []string      `yaml:"required_checks"`
	EnforceAdmins                  bool          `yaml:"enforce_admins"`
	AllowForcePushes               bool          `yaml:"allow_force_pushes"`
	AllowDeletions                 bool          `yaml:"allow_deletions"`
	RequiredLinearHistory          bool          `yaml:"required_linear_history"`
	RequiredConversationResolution bool          `yaml:"required_conversation_resolution"`
	Restrictions                   *Restrictions `yaml:"restrictions,omitempty"`
}

// Restrictions is the push allowlist for a protected branch. When set,
// only the listed actors can push directly (bypassing other rules).
// A nil value means "no allowlist configured" — anyone with write access may push.
// An empty (non-nil) value means "no one may push directly".
type Restrictions struct {
	Users []string `yaml:"users" json:"users"`
	Teams []string `yaml:"teams" json:"teams"`
	Apps  []string `yaml:"apps"  json:"apps"`
}

// Equal reports whether two restriction sets contain the same users, teams,
// and apps (order-independent).
func (r *Restrictions) Equal(o *Restrictions) bool {
	if r == nil && o == nil {
		return true
	}
	if r == nil || o == nil {
		return false
	}
	return stringSliceEqualSet(r.Users, o.Users) &&
		stringSliceEqualSet(r.Teams, o.Teams) &&
		stringSliceEqualSet(r.Apps, o.Apps)
}

// SubsetOf reports whether r is a subset of o (every user/team/app in r also
// appears in o). A nil receiver counts as "no allowlist" — not a subset of any
// non-nil allowlist.
func (r *Restrictions) SubsetOf(o *Restrictions) bool {
	if r == nil || o == nil {
		return false
	}
	return stringSliceSubset(r.Users, o.Users) &&
		stringSliceSubset(r.Teams, o.Teams) &&
		stringSliceSubset(r.Apps, o.Apps)
}

func stringSliceEqualSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}

func stringSliceSubset(sub, super []string) bool {
	seen := make(map[string]bool, len(super))
	for _, s := range super {
		seen[s] = true
	}
	for _, s := range sub {
		if !seen[s] {
			return false
		}
	}
	return true
}

// RuleDiff represents a single rule comparison result
type RuleDiff struct {
	Rule string
	Pass bool
	Want string
	Got  string
}

// NoProtectionRules returns a Rules struct representing no protection from a
// given source. Bool fields that mean "allowed" (force pushes, deletions) are
// set to true since no protection means everything is allowed.
func NoProtectionRules() Rules {
	return Rules{
		RequiredChecks:   []string{},
		AllowForcePushes: true,
		AllowDeletions:   true,
	}
}

// MergeProtective merges two Rules by taking the most protective value for
// each field. This represents the effective combined protection when both
// classic branch protection and rulesets are active.
func MergeProtective(a, b Rules) Rules {
	checks := mergeStringSlice(a.RequiredChecks, b.RequiredChecks)

	approvals := a.RequiredApprovals
	if b.RequiredApprovals > approvals {
		approvals = b.RequiredApprovals
	}

	return Rules{
		RequirePullRequest:             a.RequirePullRequest || b.RequirePullRequest,
		RequiredApprovals:              approvals,
		DismissStaleReviews:            a.DismissStaleReviews || b.DismissStaleReviews,
		RequireCodeOwnerReviews:        a.RequireCodeOwnerReviews || b.RequireCodeOwnerReviews,
		RequireStatusChecks:            a.RequireStatusChecks || b.RequireStatusChecks,
		StrictStatusChecks:             a.StrictStatusChecks || b.StrictStatusChecks,
		RequiredChecks:                 checks,
		EnforceAdmins:                  a.EnforceAdmins || b.EnforceAdmins,
		AllowForcePushes:               a.AllowForcePushes && b.AllowForcePushes,
		AllowDeletions:                 a.AllowDeletions && b.AllowDeletions,
		RequiredLinearHistory:          a.RequiredLinearHistory || b.RequiredLinearHistory,
		RequiredConversationResolution: a.RequiredConversationResolution || b.RequiredConversationResolution,
		Restrictions:                   mergeRestrictionsProtective(a.Restrictions, b.Restrictions),
	}
}

// mergeRestrictionsProtective returns the more restrictive (smaller) allowlist
// when both sources set one. A nil source means "no allowlist" — anyone may
// push — so it is not protective and the other side wins. When both are set,
// the intersection is taken since only actors in both lists could push under
// the combined policy.
func mergeRestrictionsProtective(a, b *Restrictions) *Restrictions {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &Restrictions{
		Users: intersectStrings(a.Users, b.Users),
		Teams: intersectStrings(a.Teams, b.Teams),
		Apps:  intersectStrings(a.Apps, b.Apps),
	}
}

// stringSliceOrEmpty returns the slice as-is, or a freshly allocated empty
// slice if nil. The GitHub API expects an empty JSON array (not null) for
// each principal type in a restrictions payload.
func stringSliceOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func intersectStrings(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	out := make([]string, 0)
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		if inB[s] && !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

// mergeStringSlice returns the union of a and b, deduplicated and in a stable
// order: the elements of a in order, then those of b not already present.
func mergeStringSlice(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// Default returns a Config with sensible defaults
func Default() Config {
	return Config{
		Branch: "default",
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

// UnmarshalYAML implements custom unmarshaling for OverrideRules to detect
// whether required_checks was explicitly set (distinguishing [] from absent).
func (o *OverrideRules) UnmarshalYAML(value *yaml.Node) error {
	// Use a shadow type to avoid infinite recursion
	type shadow OverrideRules
	var s shadow
	if err := value.Decode(&s); err != nil {
		return err
	}
	// yaml.Decoder.KnownFields does not reach a type with a custom
	// UnmarshalYAML, so unknown keys are rejected here instead.
	if err := checkKnownFields(value, reflect.TypeOf(OverrideRules{})); err != nil {
		return err
	}
	*o = OverrideRules(s)

	// Track which keys were explicitly set in YAML so we can distinguish
	// "absent" from "present but empty/null" for override semantics.
	for i := 0; i < len(value.Content)-1; i += 2 {
		switch value.Content[i].Value {
		case "required_checks":
			o.checksSet = true
			if o.RequiredChecks == nil {
				o.RequiredChecks = []string{}
			}
		case "restrictions":
			o.restrictionsSet = true
		}
	}
	return nil
}

// checkKnownFields reports an error for the first mapping key in node that has
// no yaml-tagged field in the struct type t, recursing into nested struct
// fields. The message mirrors yaml.v3's own unknown-field wording.
func checkKnownFields(node *yaml.Node, t reflect.Type) error {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if node.Kind != yaml.MappingNode || t.Kind() != reflect.Struct {
		return nil
	}

	fields := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields[name] = f.Type
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		ft, ok := fields[key.Value]
		if !ok {
			return fmt.Errorf("line %d: field %s not found in type %s", key.Line, key.Value, t.String())
		}
		if err := checkKnownFields(val, ft); err != nil {
			return err
		}
	}
	return nil
}

// Load reads and parses a rampart config file
func Load(configPath string) (Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// An empty file yields io.EOF and means "no configuration", matching the
	// previous yaml.Unmarshal behavior.
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("failed to parse config: %w", err)
	}

	if cfg.Branch == "" {
		cfg.Branch = "default"
	}
	if cfg.Rules.RequiredChecks == nil {
		cfg.Rules.RequiredChecks = []string{}
	}

	// Reject malformed globs up front: matchesRepo treats a bad pattern as a
	// non-match, which would silently drop the intended exception.
	for i, o := range cfg.Overrides {
		for _, pattern := range o.Repos {
			if _, err := path.Match(pattern, ""); err != nil {
				return Config{}, fmt.Errorf("invalid repo pattern %q in overrides[%d]: %w", pattern, i, err)
			}
		}
	}

	return cfg, nil
}

// RulesForRepo returns the effective rules for a given repo name by starting
// with the base rules and applying any matching overrides in order.
func (c Config) RulesForRepo(repoName string) Rules {
	rules := c.Rules
	for _, o := range c.Overrides {
		if o.matchesRepo(repoName) {
			rules = mergeOverride(rules, o.Rules)
		}
	}
	return rules
}

// matchesRepo returns true if the repo name matches any of the override's patterns.
func (o Override) matchesRepo(repoName string) bool {
	for _, pattern := range o.Repos {
		if matched, err := path.Match(pattern, repoName); err == nil && matched {
			return true
		}
	}
	return false
}

// mergeOverride applies non-nil override fields on top of base rules.
func mergeOverride(base Rules, o OverrideRules) Rules {
	if o.RequirePullRequest != nil {
		base.RequirePullRequest = *o.RequirePullRequest
	}
	if o.RequiredApprovals != nil {
		base.RequiredApprovals = *o.RequiredApprovals
	}
	if o.DismissStaleReviews != nil {
		base.DismissStaleReviews = *o.DismissStaleReviews
	}
	if o.RequireCodeOwnerReviews != nil {
		base.RequireCodeOwnerReviews = *o.RequireCodeOwnerReviews
	}
	if o.RequireStatusChecks != nil {
		base.RequireStatusChecks = *o.RequireStatusChecks
	}
	if o.StrictStatusChecks != nil {
		base.StrictStatusChecks = *o.StrictStatusChecks
	}
	if o.checksSet {
		base.RequiredChecks = o.RequiredChecks
	}
	if o.EnforceAdmins != nil {
		base.EnforceAdmins = *o.EnforceAdmins
	}
	if o.AllowForcePushes != nil {
		base.AllowForcePushes = *o.AllowForcePushes
	}
	if o.AllowDeletions != nil {
		base.AllowDeletions = *o.AllowDeletions
	}
	if o.RequiredLinearHistory != nil {
		base.RequiredLinearHistory = *o.RequiredLinearHistory
	}
	if o.RequiredConversationResolution != nil {
		base.RequiredConversationResolution = *o.RequiredConversationResolution
	}
	if o.restrictionsSet {
		base.Restrictions = o.Restrictions
	}
	return base
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
	var restrictions interface{}
	if r.Restrictions != nil {
		restrictions = map[string]interface{}{
			"users": stringSliceOrEmpty(r.Restrictions.Users),
			"teams": stringSliceOrEmpty(r.Restrictions.Teams),
			"apps":  stringSliceOrEmpty(r.Restrictions.Apps),
		}
	}

	payload := map[string]interface{}{
		"enforce_admins":                   r.EnforceAdmins,
		"allow_force_pushes":               r.AllowForcePushes,
		"allow_deletions":                  r.AllowDeletions,
		"required_linear_history":          r.RequiredLinearHistory,
		"required_conversation_resolution": r.RequiredConversationResolution,
		"restrictions":                     restrictions,
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
	Restrictions *struct {
		Users []struct {
			Login string `json:"login"`
		} `json:"users"`
		Teams []struct {
			Slug string `json:"slug"`
		} `json:"teams"`
		Apps []struct {
			Slug string `json:"slug"`
		} `json:"apps"`
	} `json:"restrictions"`
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

	if resp.Restrictions != nil {
		restr := &Restrictions{
			Users: make([]string, 0, len(resp.Restrictions.Users)),
			Teams: make([]string, 0, len(resp.Restrictions.Teams)),
			Apps:  make([]string, 0, len(resp.Restrictions.Apps)),
		}
		for _, u := range resp.Restrictions.Users {
			restr.Users = append(restr.Users, u.Login)
		}
		for _, t := range resp.Restrictions.Teams {
			restr.Teams = append(restr.Teams, t.Slug)
		}
		for _, a := range resp.Restrictions.Apps {
			restr.Apps = append(restr.Apps, a.Slug)
		}
		r.Restrictions = restr
	}

	return r
}

// Compare compares desired rules against actual rules and returns diffs.
// When allowStricter is true, a repo that exceeds the minimum config
// (e.g. more required approvals, extra protections enabled) is treated as passing.
func Compare(desired, actual Rules, allowStricter bool) []RuleDiff {
	var diffs []RuleDiff

	addDiff := func(rule string, pass bool, want, got string) {
		diffs = append(diffs, RuleDiff{Rule: rule, Pass: pass, Want: want, Got: got})
	}

	// For bool rules where true is "stricter" (more protective):
	// pass if equal, or if allowStricter and actual is true while desired is false.
	addStricterBoolDiff := func(rule string, want, got bool) {
		pass := want == got || (allowStricter && got && !want)
		addDiff(rule, pass, fmt.Sprintf("%t", want), fmt.Sprintf("%t", got))
	}

	// For bool rules where false is "stricter" (more protective), i.e. allow_* rules:
	// pass if equal, or if allowStricter and actual is false while desired is true.
	addLooserBoolDiff := func(rule string, want, got bool) {
		pass := want == got || (allowStricter && !got && want)
		addDiff(rule, pass, fmt.Sprintf("%t", want), fmt.Sprintf("%t", got))
	}

	// Pull request reviews
	addStricterBoolDiff("require_pull_request", desired.RequirePullRequest, actual.RequirePullRequest)
	if desired.RequirePullRequest {
		approvalPass := desired.RequiredApprovals == actual.RequiredApprovals ||
			(allowStricter && actual.RequiredApprovals > desired.RequiredApprovals)
		addDiff("required_approvals", approvalPass,
			fmt.Sprintf("%d", desired.RequiredApprovals),
			fmt.Sprintf("%d", actual.RequiredApprovals))
		addStricterBoolDiff("dismiss_stale_reviews", desired.DismissStaleReviews, actual.DismissStaleReviews)
		addStricterBoolDiff("require_code_owner_reviews", desired.RequireCodeOwnerReviews, actual.RequireCodeOwnerReviews)
	}

	// Status checks
	addStricterBoolDiff("require_status_checks", desired.RequireStatusChecks, actual.RequireStatusChecks)
	if desired.RequireStatusChecks {
		addStricterBoolDiff("strict_status_checks", desired.StrictStatusChecks, actual.StrictStatusChecks)
		// Compare required checks: actual must contain all desired checks.
		// With allowStricter, actual may contain additional checks beyond desired.
		allDesiredPresent := true
		for _, c := range desired.RequiredChecks {
			found := false
			for _, ac := range actual.RequiredChecks {
				if ac == c {
					found = true
					break
				}
			}
			if !found {
				allDesiredPresent = false
				break
			}
		}
		var checksPass bool
		if allowStricter {
			checksPass = allDesiredPresent
		} else {
			checksPass = allDesiredPresent && len(desired.RequiredChecks) == len(actual.RequiredChecks)
		}
		addDiff("required_checks", checksPass,
			fmt.Sprintf("%v", desired.RequiredChecks),
			fmt.Sprintf("%v", actual.RequiredChecks))
	}

	// Other rules
	addStricterBoolDiff("enforce_admins", desired.EnforceAdmins, actual.EnforceAdmins)
	addLooserBoolDiff("allow_force_pushes", desired.AllowForcePushes, actual.AllowForcePushes)
	addLooserBoolDiff("allow_deletions", desired.AllowDeletions, actual.AllowDeletions)
	addStricterBoolDiff("required_linear_history", desired.RequiredLinearHistory, actual.RequiredLinearHistory)
	addStricterBoolDiff("required_conversation_resolution", desired.RequiredConversationResolution, actual.RequiredConversationResolution)

	// Restrictions: only compared when the config explicitly opts in by
	// setting `restrictions`. Without that, omit the diff entirely so configs
	// that don't care about push allowlists behave exactly as before.
	if desired.Restrictions != nil {
		var pass bool
		switch {
		case actual.Restrictions == nil:
			pass = false
		case desired.Restrictions.Equal(actual.Restrictions):
			pass = true
		case allowStricter:
			// A stricter (smaller) allowlist is a subset of the configured one.
			pass = actual.Restrictions.SubsetOf(desired.Restrictions)
		default:
			pass = false
		}
		addDiff("restrictions", pass,
			formatRestrictions(desired.Restrictions),
			formatRestrictions(actual.Restrictions))
	}

	return diffs
}

func formatRestrictions(r *Restrictions) string {
	if r == nil {
		return "<none>"
	}
	return fmt.Sprintf("users=%v teams=%v apps=%v",
		stringSliceOrEmpty(r.Users),
		stringSliceOrEmpty(r.Teams),
		stringSliceOrEmpty(r.Apps))
}
