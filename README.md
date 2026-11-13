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
  # Optional push allowlist — only the listed actors can push directly.
  # restrictions:
  #   users: [wdm0006]
  #   teams: []
  #   apps:  [renovate]
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

Check all repos for the given user/org against your config. Shows pass/fail per rule for each repo. By default, exits non-zero if any repos are non-compliant or cannot be read (useful in CI). With `--baseline`, only new drift and read errors fail.

Options:
- `--repo NAME` — audit a single repo
- `--exclude NAME` — exclude repos (repeatable)
- `--config FILE` — config path (default: `rampart.yaml`)
- `--baseline FILE` — fail only on drift not recorded in the baseline; read errors always fail
- `--write-baseline FILE` — record current failing rules; exits non-zero on read errors. Cannot be combined with `--baseline`
- `--report FILE` — write a self-contained HTML report to the given path
- `--concurrency N` — repos read in parallel (default `4`, minimum `1`; `1` is serial). Output order is unchanged. Keep it low on large fleets to stay under GitHub rate limits
- `--format text|json` — output format (default `text`); `json` is written to stdout, progress lines go to stderr

**Repo visibility.** With `--owner` omitted (or set to your own login), rampart audits every repo you own, public and private. For an organization, it audits every repo your token can see, including private ones. Auditing *another* user's account can only ever see their **public** repos — GitHub's user-repository API exposes nothing else, regardless of token scopes.

### `rampart config`

Show detailed documentation for all `rampart.yaml` configuration options.

### `rampart apply --owner NAME`

Apply your config to any non-compliant repos. After successful writes, Rampart re-audits each repository and reports `verified` only when it is compliant. Residual drift or verification read failures count as failures and exit non-zero. `--dry-run` previews changes without writes or post-write verification.

Apply creates or updates the managed `rampart` ruleset, which can only add protection. If classic branch protection or another ruleset makes a repository stricter than the config, apply still writes any fixable changes but reports the repository as failed and exits non-zero. Relax the named rules manually or set `allow_stricter_rules: true` to accept them.

Options:
- `--repo NAME` — apply to a single repo
- `--exclude NAME` — exclude repos (repeatable)
- `--config FILE` — config path (default: `rampart.yaml`)
- `--concurrency N` — repos read in parallel (default `4`, minimum `1`; `1` is serial). Output order is unchanged. Keep it low on large fleets to stay under GitHub rate limits
- `--dry-run` — preview changes without applying

## How it works

1. Reads your `rampart.yaml` config
2. Lists all non-fork, non-archived repos for the owner
3. Fetches current branch protection from both classic protection and rulesets
4. Merges them (most restrictive combination wins)
5. Compares effective rules against desired config
6. Reports compliance (audit) or applies fixes via rulesets and re-audits to verify protection (apply)

Rampart supports both **classic branch protection** and the newer **GitHub rulesets API**. During audit, both sources are checked. During apply, rampart creates or updates a repository ruleset named "rampart".

All GitHub API calls go through the `gh` CLI, so authentication is handled by your existing `gh auth` session.

## Use in CI

```yaml
- name: Audit branch protection
  run: rampart audit --owner myorg --config rampart.yaml
```

By default, `audit` exits non-zero on drift or read errors. To adopt it on a fleet with existing drift, record failures with `--write-baseline rampart-baseline.json`, commit the file, then use `--baseline rampart-baseline.json` to gate on new drift. Read errors always fail. See the [baseline guide](docs/ci-drift.md#adopting-on-a-fleet-with-existing-drift-baseline) for details.

For machine-readable results, use `--format json`:

```yaml
- name: Audit branch protection
  run: rampart audit --owner myorg --format json > audit.json
```

Schema (stable key order; repos appear in audit order, diffs in check order; exit codes are unchanged):

```json
{
  "owner": "myorg",
  "config": "rampart.yaml",
  "branch": "default",
  "summary": {"compliant": 1, "non_compliant": 2, "skipped": 1, "total": 4},
  "repos": [
    {
      "repo": "api",
      "branch": "main",
      "status": "non_compliant",
      "error": "",
      "diffs": [{"rule": "required_approvals", "pass": false, "want": "2", "got": "1", "baselined": false}]
    }
  ]
}
```

- `status` is one of `compliant`, `non_compliant`, `skipped`, `error`. `error` is empty unless the repo was skipped or errored; `diffs` is `[]` when no checks ran.
- `summary` uses the same counts as the terminal and HTML summaries: errored repos count as `non_compliant`, and `total` includes skipped repos.
- Each diff includes `baselined`, true only for a failing rule recorded in the supplied baseline. With `--baseline`, `baseline_fixed` lists entries that now pass when any are found; remove those entries to ratchet adoption. Baselines affect exit status, while summary counts still reflect actual protection.
- An invalid `--format` value is rejected before any GitHub call.

In GitHub Actions, `audit` automatically appends counts and up to 50 failing-rule/error rows to the job summary when `GITHUB_STEP_SUMMARY` is set, with either output format.

For a scheduled drift check that publishes the HTML report as an artifact, see [docs/ci-drift.md](docs/ci-drift.md) and [examples/rampart-drift.yml](examples/rampart-drift.yml).
