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

## Commands

### `rampart init`

Generate a `rampart.yaml` with sensible defaults in the current directory.

### `rampart audit --owner NAME`

Check all repos for the given user/org against your config. Shows pass/fail per rule for each repo. Exits non-zero if any repos are non-compliant (useful in CI).

Options:
- `--repo NAME` — audit a single repo
- `--exclude NAME` — exclude repos (repeatable)
- `--config FILE` — config path (default: `rampart.yaml`)

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
3. Fetches current branch protection for each repo
4. Compares actual rules against desired rules
5. Reports compliance (audit) or applies fixes (apply)

All GitHub API calls go through the `gh` CLI, so authentication is handled by your existing `gh auth` session.

## Use in CI

```yaml
- name: Audit branch protection
  run: rampart audit --owner myorg --config rampart.yaml
```

The `audit` command exits non-zero when any repos are non-compliant, making it easy to use as a CI check.
