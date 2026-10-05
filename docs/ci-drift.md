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
- Uploads the report with `if: always()`, so it is available when the audit fails, which is when you need it. `if-no-files-found: warn` covers failures before the report is written (for example a bad config).
- Linux runner only; macOS and Windows runners bill at a higher rate and add nothing here.

## Exit codes

| Exit | Meaning |
| --- | --- |
| `0` | Every audited repo is compliant (skipped repos do not count). |
| `1` | At least one repo is non-compliant or could not be read, or the command itself failed (bad config, unknown flag). |

The job log and report distinguish drift from read errors; the exit code does not.

## Read-only audit, manual remediation

`audit` only reads. To converge drift, review the report, then run `rampart apply --owner OWNER --dry-run` and `rampart apply --owner OWNER` yourself with a token that has write access. Scheduled auto-remediation is deliberately not part of this example.
