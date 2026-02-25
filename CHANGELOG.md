# Changelog

## v0.5.0

### Features

- Add per-repo rule overrides with glob pattern support. Use the `overrides` section in `rampart.yaml` to apply different rules to repos matching patterns like `prod-*` or `infra-*`. Only specified fields are overridden; everything else inherits from the base config. Overrides work with both `audit` and `apply`.
- Update `rampart config` help to document the new overrides section.

## v0.4.1

### Features

- Add `rampart config` command that displays a detailed reference for all `rampart.yaml` configuration options, including types, defaults, descriptions, and an example config.

## v0.4.0

### Bug Fixes

- Fix organization owner support: `--owner` now correctly lists all accessible repos (including private) when the owner is a GitHub organization. Previously, the `users/` API endpoint was used for orgs, which silently returned only public repos.

### Features

- Add `allow_stricter_rules` config option: when set to `true`, repos that exceed your minimum protection config are treated as compliant. For example, if you require 1 approval but a repo requires 2, it will pass instead of being flagged as non-compliant.

## v0.3.0

- Add `--report` flag to audit command for self-contained HTML report output.

## v0.1.0

- Initial release: `audit`, `apply`, and `init` commands.
- YAML-based config for branch protection rules.
- Support for `default` branch resolution.
- Homebrew tap distribution.
