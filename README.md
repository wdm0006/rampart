# Rampart

Audit and enforce GitHub branch protection rules across all repos for a user or organization.

Define your desired protection rules in a YAML config, then run `rampart audit` to check compliance or `rampart apply` to fix non-compliant repos.

## Installation

### Homebrew

```bash
brew install wdm0006/tap/rampart
```

### Binary releases

Download from [GitHub Releases](https://github.com/wdm0006/rampart/releases).

### Go install

```bash
go install github.com/wdm0006/rampart/cmd/rampart@latest
```

### Prerequisites

- [GitHub CLI](https://cli.github.com) (`gh`) installed and authenticated
- Admin access to the repos you want to manage

## Quick start

```bash
# Generate a default config
rampart init

# Edit rampart.yaml to customize rules
# ...

# Audit all your repos
rampart audit --owner myuser

# Fix non-compliant repos
rampart apply --owner myuser
```

## Config format

`rampart.yaml` defines the branch and protection rules to enforce:

```yaml
branch: default
allow_stricter_rules: false
rules:
  require_pull_request: true
  required_approvals: 1
  dismiss_stale_reviews: true
  require_code_owner_reviews: false
  require_status_checks: false
  strict_status_checks: true
  required_checks: []
  enforce_admins: true
  allow_force_pushes: false
  allow_deletions: false
  required_linear_history: false
  required_conversation_resolution: false
```

Setting `branch: default` resolves to each repo's actual default branch (e.g., `main` or `master`). You can also specify an exact branch name like `main` if preferred.

Setting `allow_stricter_rules: true` treats repos as compliant when their protection is stricter than your config. For example, if you require 1 approval but a repo requires 2, it passes. This applies directionally per rule — more approvals, extra required checks, and disabling force pushes are all considered "stricter".

### Per-repo overrides

Use `overrides` to apply different rules to specific repos. Patterns support exact names and globs (`*`, `?`). Only specified fields are overridden; everything else inherits from the base rules. Overrides are applied in order, so later entries win for overlapping fields.

```yaml
rules:
  required_approvals: 1
  # ... base rules
overrides:
  - repos: ["prod-*", "infra-*"]
    rules:
      required_approvals: 2
      require_code_owner_reviews: true
  - repos: ["docs-site"]
    rules:
      require_pull_request: false
```

Run `rampart config` for detailed documentation of all configuration options.

## Commands

### `rampart init`

Generate a `rampart.yaml` with sensible defaults in the current directory.

### `rampart audit --owner NAME`

Check all repos for the given user/org against your config. Shows pass/fail per rule for each repo. Exits non-zero if any repos are non-compliant (useful in CI).

Options:
- `--repo NAME` — audit a single repo
- `--exclude NAME` — exclude repos (repeatable)
- `--config FILE` — config path (default: `rampart.yaml`)
- `--report FILE` — write a self-contained HTML report to the given path

### `rampart config`

Show detailed documentation for all `rampart.yaml` configuration options.

### `rampart apply --owner NAME`

Apply your config to any non-compliant repos.

Options:
- `--repo NAME` — apply to a single repo
- `--exclude NAME` — exclude repos (repeatable)
- `--config FILE` — config path (default: `rampart.yaml`)
- `--dry-run` — preview changes without applying

## How it works

1. Reads your `rampart.yaml` config
2. Lists all non-fork, non-archived repos for the owner
3. Fetches current branch protection from both classic protection and rulesets
4. Merges them (most restrictive combination wins)
5. Compares effective rules against desired config
6. Reports compliance (audit) or applies fixes via rulesets (apply)

Rampart supports both **classic branch protection** and the newer **GitHub rulesets API**. During audit, both sources are checked. During apply, rampart creates or updates a repository ruleset named "rampart".

All GitHub API calls go through the `gh` CLI, so authentication is handled by your existing `gh auth` session.

## Use in CI

```yaml
- name: Audit branch protection
  run: rampart audit --owner myorg --config rampart.yaml
```

The `audit` command exits non-zero when any repos are non-compliant, making it easy to use as a CI check.
