# Copilot Instructions for getRelease

## Project Overview

getRelease is a Go 1.25 CLI tool that downloads, extracts, and installs binary releases from GitHub repositories. It replaces an existing Zsh autoload workflow with a Cobra-based application backed by Viper for configuration, structured install history, history-backed shell completion, and upgrade workflows.

## Architecture

```text
main.go                        → Entry point, calls cmd.Execute()
cmd/root.go                    → Root install command, flag setup, Viper init, repo resolution
cmd/completion.go              → History-backed shell completion helpers
cmd/config.go                  → config subcommands: show/get/set/edit/reset/path
cmd/history.go                 → history subcommands: list/remove/clear/prune/edit/path
cmd/list.go                    → list subcommand: list releases or assets
cmd/upgrade.go                 → upgrade subcommand: single target or --all upgrade flow
cmd/version.go                 → version subcommand
internal/archive/extract.go    → Archive extraction and binary discovery
internal/config/config.go      → AppConfig type, Viper init/load/save
internal/config/paths.go       → XDG/platform path resolution
internal/github/client.go      → GitHubClient HTTP wrapper
internal/github/release.go     → Release, Asset types
internal/github/parse.go       → URL → owner/repo parsing
internal/platform/detect.go    → OS/arch detection
internal/platform/install_name.go → Installed binary name normalization
internal/platform/match.go     → Asset matching heuristics
internal/history/store.go      → Persistent install history store
internal/install/install.go    → Binary installation backends
internal/selector/selector.go  → Interactive selection and confirmation prompts
```

**Key types:**

- `config.AppConfig` – runtime configuration populated from Viper (file + env + flags)
- `config.AssetPreferences` – OS/arch/format preferences for asset filtering
- `github.Client` – HTTP wrapper for GitHub Releases API
- `github.Release` / `github.Asset` – API response types
- `history.Store` – persisted install history used by upgrade and completion
- `history.Record` – installed release metadata keyed by owner/repo
- `install.Installer` – binary installation abstraction
- `platform.Info` – normalized OS/arch pair

**Command surface:**

- `getRelease` installs the latest or requested tagged release.
- `getRelease list` lists releases or assets for a repository.
- `getRelease upgrade` upgrades one installed target or every installed target with `--all`.
- `getRelease history` inspects and maintains recorded installs.
- `getRelease config` inspects and changes persisted configuration.
- `getRelease completion` emits shell completion scripts with history-backed suggestions.

**Data flow:** install command flags → resolveRepo() → GitHubClient → filter assets → select → download → extract → select binaries → install → history

**Upgrade flow:** install history → resolve target → fetch latest release → match replacement asset → download/extract → reinstall recorded binaries → update history

**Exit codes:** `0` = success, `1` = operational error, `2` = user cancelled

## Code Patterns

**Error handling:** Wrap errors with context using `fmt.Errorf("description: %w", err)`

**Logging:** Use `log/slog` (stdlib):

```go
slog.Info("resolved repository", "owner", owner, "repo", repo)
slog.Debug("matching assets", "count", len(assets))
```

**Adding CLI flags:** Define in `init()`, use `rootCmd.Flags()` for command-specific or `PersistentFlags()` for global. Mutually exclusive flags use `MarkFlagsMutuallyExclusive()`. Bind to Viper with `cfgViper.BindPFlag()`.

**Configuration:** Backed by Viper with precedence: CLI flags > env vars (GETRELEASE_*) > config.yaml > built-in defaults.

**History and completion:** Install history is stored under the platform data directory and is used by `upgrade`, owner/repo flag completion, and installed-target completion.

**Output formats:** Support text (default) and JSON via `--format` on the main install flow, list output, history list, and config show.

## Development Commands

```bash
go test ./... -v           # Run tests
go test ./... -cover       # Coverage (must maintain ≥80%)
go build -o getRelease .   # Build binary
go vet ./...               # Static analysis
```

## Testing Patterns

Use `httptest.Server` with recorded JSON fixtures for GitHub API tests. Test the `cmd` package using Cobra's `Execute()` with captured stdout. Use table-driven tests with `t.Parallel()`.

Example test structure:

```go
func TestParseRepoURL(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name      string
        url       string
        wantOwner string
        wantRepo  string
        wantErr   bool
    }{
        {name: "full URL", url: "https://github.com/owner/repo", wantOwner: "owner", wantRepo: "repo"},
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            owner, repo, err := github.ParseRepoURL(tt.url)
            // assertions...
        })
    }
}
```

## Commit Convention

Uses [Conventional Commits](https://www.conventionalcommits.org/) (enforced by pre-commit hooks).
