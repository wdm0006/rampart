# Contributing to Rampart

## Getting started

1. Clone the repo
2. Make sure you have Go 1.21+ and `gh` CLI installed
3. Build: `go build -o rampart ./cmd/rampart`
4. Test: `go test ./...`

## Before submitting a PR

```bash
gofmt -w .
go vet ./...
go test -race ./...
go build -o rampart ./cmd/rampart
```

## Project structure

```
rampart/
├── cmd/rampart/main.go          # Entry point (ldflags version)
├── internal/
│   ├── cli/
│   │   ├── root.go              # Root command, version, Execute()
│   │   ├── init.go              # Generate default rampart.yaml
│   │   ├── audit.go             # Audit repos + shared auditRepos() engine
│   │   └── apply.go             # Apply rules to non-compliant repos
│   ├── github/
│   │   └── repos.go             # gh api: list repos, get/set branch protection
│   └── config/
│       └── config.go            # YAML config parsing, API payload, comparison
├── .goreleaser.yaml
├── .github/workflows/
│   ├── ci.yml
│   └── release.yml
├── go.mod
├── LICENSE
├── README.md
└── CONTRIBUTING.md
```

## Code style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Keep error messages lowercase (Go convention)
- All GitHub API calls go through `gh` CLI (no direct HTTP/SDK)

## Releasing

Releases are automated via GoReleaser. To create a release:

1. Tag: `git tag v0.1.0`
2. Push: `git push origin v0.1.0`
3. GoReleaser builds binaries and updates the Homebrew tap

## Commit messages

Use conventional-style messages:

- `Add audit command with per-rule diff output`
- `Fix 404 handling for unprotected branches`
- `Update default config values`
