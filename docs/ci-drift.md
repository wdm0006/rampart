# Scheduled drift check in GitHub Actions

[`examples/rampart-drift.yml`](../examples/rampart-drift.yml) is a copy-pasteable workflow that audits your fleet on a schedule and publishes the HTML report as an artifact.

## Setup

1. Commit your `rampart.yaml` to the repo that will run the workflow.
2. Copy the example to `.github/workflows/rampart-drift.yml` and set `OWNER` (and the cron time).
3. Create a token (see below) and store it as the repository secret `RAMPART_TOKEN`.
4. Run it once from the **Actions** tab (`workflow_dispatch`) to confirm.

## Token

The workflow's built-in `GITHUB_TOKEN` is scoped to the repository running the workflow. It cannot read branch protection or rulesets of other repositories, so the audit step uses a separate token exported as `GH_TOKEN` (which `gh` reads automatically).

The token owner needs admin access to every repo in scope; GitHub only returns branch protection to admins.

- **Fine-grained PAT:** repository access to the repos in scope, with `Administration: Read-only` and `Metadata: Read-only`. For an organization, the org must allow fine-grained tokens.
- **Classic PAT:** the `repo` scope.

Use a read-only token for this workflow. `audit` never writes. Keep any token that can run `apply` out of the scheduled job.

### Forks and pull requests

Secrets are not available to workflows triggered from forks, so a fork's runs cannot audit anything. The example therefore triggers only on `schedule` and `workflow_dispatch`, never `pull_request`. Scheduled workflows run only on the default branch, and GitHub disables them after 60 days without repository activity in public repos.

## What the workflow does

- `permissions: contents: read` only; the audit authenticates with `RAMPART_TOKEN`, not the job token.
- Installs the latest release archive for Linux amd64 with `gh release download` (preinstalled `gh` on `ubuntu-latest`) and verifies it against `checksums.txt`. Pin `tag` to a version to make runs reproducible.
- Runs `rampart audit --report rampart-report.html`.
- Automatically appends audit counts and a table of failing rules/read errors to the Actions job summary when `GITHUB_STEP_SUMMARY` is set. The table shows at most 50 rows, followed by a `+N more rows` line; download the HTML report for full details. This works with both text and JSON output, keeps JSON stdout pure, and only warns on stderr if the summary cannot be written. Outside Actions, no summary is written unless that environment variable is set.
- Uploads the report with `if: always()`, so it is available when the audit fails, which is when you need it. `if-no-files-found: warn` covers failures before the report is written (for example a bad config).
- Linux runner only; macOS and Windows runners bill at a higher rate and add nothing here.

## Exit codes

| Exit | Meaning |
| --- | --- |
| `0` | Every audited repo is compliant (skipped repos do not count). |
| `1` | At least one repo is non-compliant or could not be read, or the command itself failed (bad config, unknown flag). |

The job summary, log and report distinguish drift from read errors; the exit code does not.

## Adopting on a fleet with existing drift (baseline)

To gate only on new drift, record the current failures once and commit the file:

```sh
rampart audit --owner OWNER --write-baseline rampart-baseline.json
```

Then audit with `--baseline`:

```sh
rampart audit --owner OWNER --baseline rampart-baseline.json --report rampart-report.html
```

- A failing rule listed for that repo in the baseline is reported as `(baselined)` (and `"baselined": true` per diff in `--format json`) and does not affect the exit code.
- Any failing rule not in the baseline, a new failing repo, or any read error exits `1`. Repos that failed to read are never written to a baseline, so a read failure can never be grandfathered.
- Baseline entries that now pass are listed as `fixed - remove from baseline` (also `baseline_fixed` in JSON); this is informational. Re-run `--write-baseline` to ratchet the file down.
- A missing, unreadable or malformed baseline file is an error (exit `1`), never an empty baseline.
- `--write-baseline` exits `1` only if a repo could not be read; recorded drift does not fail that run. `--baseline` and `--write-baseline` cannot be combined.

## Read-only audit, manual remediation

`audit` only reads. To converge drift, review the report, then run `rampart apply --owner OWNER --dry-run` and `rampart apply --owner OWNER` yourself with a token that has write access. Scheduled auto-remediation is deliberately not part of this example.

## Rate limits

Large fleets can hit GitHub's primary or secondary rate limits. Rampart
retries rate-limited reads up to three times with capped exponential backoff
and jitter (`apply` writes are never retried). If the limit persists, the
repository is reported as an error (`GitHub API rate limit exceeded; retry
later or lower --concurrency`) and the run exits 1; this is distinct from an
`insufficient permissions` error, so do not widen the token for it. If it
recurs on a scheduled run, lower `--concurrency` (default 4).

## Comparing with the previous run (`--compare-to`)

`--baseline` records accepted debt; `--compare-to` answers "what changed since the last run". Save the JSON output as an artifact and feed it back on the next run:

```bash
rampart audit --owner OWNER --format json --compare-to previous/audit.json > audit.json
```

Upload `audit.json` with `actions/upload-artifact` on every run, and download the previous run's artifact (for example with `gh run download` or `actions/download-artifact` with `run-id`) into `previous/` before auditing. Skip `--compare-to` on the first run, when no artifact exists yet.

- Per repo, `newly_failing` and `newly_fixed` (omitted when empty) list rules that started or stopped failing; top-level `compare` lists `new_repos` and `removed_repos`. Text output and the Actions summary get a matching section.
- A repo new to the fleet reports all its failing rules as newly failing.
- Rules are compared only when the repo was readable (compliant or non-compliant) in both runs: an errored or skipped repo in either run reports nothing, so an unreadable previous state is never reported as "newly fixed".
- A rule absent from the current policy is not reported as fixed.
- The exit code is unchanged (current failures, or `--baseline`). A missing or malformed compare file exits `1` before any auditing.
