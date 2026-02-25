package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show detailed help for rampart.yaml configuration",
	Long: `Rampart is configured with a YAML file (default: rampart.yaml). Run
'rampart init' to generate one with sensible defaults, then customize it.

TOP-LEVEL OPTIONS
=================

branch (string, default: "default")
  The branch to audit and enforce protection rules on.

  Set to "default" to automatically resolve to each repo's actual default
  branch (e.g. "main", "master", "develop"). This is the recommended
  setting since different repos may use different default branch names.

  Set to a specific branch name (e.g. "main") to target that exact branch
  across all repos.

allow_stricter_rules (bool, default: false)
  When true, repos with protection rules stricter than the config are
  treated as compliant. This lets you set a minimum baseline without
  flagging repos that exceed it.

  For example, if the config requires 1 approval but a repo requires 2,
  it passes. If the config allows force pushes but a repo disallows them,
  it passes (disallowing is stricter).

  Strictness direction per rule:
    - More protective = stricter for most rules (true > false)
    - For allow_force_pushes and allow_deletions, false is stricter
    - For required_approvals, a higher number is stricter
    - For required_checks, a superset is stricter

RULES
=====

Pull Request Rules
------------------

require_pull_request (bool, default: true)
  Require a pull request before merging. When false, direct pushes to the
  protected branch are allowed. When true, the following sub-rules apply.

required_approvals (int, default: 1)
  Minimum number of approving reviews required on a pull request before
  it can be merged. Set to 0 to require PRs but not approvals. Only
  evaluated when require_pull_request is true.

dismiss_stale_reviews (bool, default: true)
  Automatically dismiss existing approvals when new commits are pushed
  to the pull request. This forces re-review of changed code. Only
  evaluated when require_pull_request is true.

require_code_owner_reviews (bool, default: false)
  Require an approving review from a designated code owner (as defined
  in a CODEOWNERS file) before merging. Only evaluated when
  require_pull_request is true.

Status Check Rules
------------------

require_status_checks (bool, default: false)
  Require status checks (e.g. CI builds, tests) to pass before merging.
  When true, the following sub-rules apply.

strict_status_checks (bool, default: true)
  Require the branch to be up-to-date with the base branch before
  merging. This ensures CI ran against the latest code. Only evaluated
  when require_status_checks is true.

required_checks (list of strings, default: [])
  Specific status check contexts that must pass before merging. These
  are the names of CI jobs or checks as they appear in GitHub (e.g.
  "build", "test", "lint"). Only evaluated when require_status_checks
  is true.

  Example:
    required_checks:
      - build
      - test
      - lint

Other Rules
-----------

enforce_admins (bool, default: true)
  Apply branch protection rules to repository administrators. When false,
  admins can bypass all protection rules including required reviews and
  status checks.

allow_force_pushes (bool, default: false)
  Allow force pushes to the protected branch. Force pushes rewrite
  history and can cause data loss. Setting this to false is the safer
  option.

allow_deletions (bool, default: false)
  Allow the protected branch to be deleted. Setting this to false
  prevents accidental branch deletion.

required_linear_history (bool, default: false)
  Require a linear commit history on the protected branch (no merge
  commits). This enforces squash or rebase merging strategies.

required_conversation_resolution (bool, default: false)
  Require all review conversations on a pull request to be resolved
  before merging. This ensures all feedback has been addressed.

OVERRIDES
=========

overrides (list, default: none)
  Per-repo rule overrides. Each entry has a list of repo name patterns
  and a sparse set of rules that override the base config for matching
  repos. Only the fields you specify are overridden; everything else
  is inherited from the base rules.

  Patterns support exact names and shell-style globs:
    - "my-repo"          exact match
    - "prod-*"           prefix wildcard
    - "*-critical"       suffix wildcard
    - "svc-??-prod"      single-character wildcards

  If multiple overrides match a repo, they are applied in order (later
  entries win for overlapping fields).

  Overrides affect both audit (comparison) and apply (what gets written).

  Example:
    overrides:
      - repos: ["prod-*", "infra-*"]
        rules:
          required_approvals: 2
          enforce_admins: true
      - repos: ["docs-site"]
        rules:
          require_pull_request: false

EXAMPLE CONFIG
==============

  branch: default
  allow_stricter_rules: true
  rules:
    require_pull_request: true
    required_approvals: 1
    dismiss_stale_reviews: true
    require_code_owner_reviews: false
    require_status_checks: true
    strict_status_checks: true
    required_checks:
      - build
      - test
    enforce_admins: true
    allow_force_pushes: false
    allow_deletions: false
    required_linear_history: false
    required_conversation_resolution: false
  overrides:
    - repos: ["prod-*", "infra-*"]
      rules:
        required_approvals: 2
        require_code_owner_reviews: true
    - repos: ["docs-site"]
      rules:
        require_pull_request: false`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(cmd.Long)
	},
}
