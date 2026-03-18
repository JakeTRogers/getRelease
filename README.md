# getRelease

## Description

getRelease is a Go CLI for downloading GitHub release assets, extracting archives, installing binaries, and tracking what was installed so later upgrades can be automated. It can target a repository by owner and repo name or by full GitHub URL, prefers assets that match the current platform, and falls back to interactive selection when more than one candidate fits.

## Features

- Install the latest release or a specific tag from any GitHub repository.
- Target repositories with `--owner` and `--repo` or a single `--url`.
- Match assets by operating system, architecture, and preferred archive formats.
- Auto-select the best asset when there is a clear winner, otherwise prompt interactively.
- Extract common archive formats including `.tar.gz`, `.tar.xz`, `.tar.bz2`, `.tar`, and `.zip`.
- Install one binary or multiple binaries from the selected asset.
- Rename a single installed binary with `--install-as`.
- Skip installation and keep the downloaded payload with `--download-only`.
- Track install history for upgrades, shell completion, and cleanup workflows.
- Upgrade one installed package or every installed package still present on disk.
- Manage config and history from built-in `config` and `history` subcommands.
- Emit machine-readable JSON from the main install flow and list/history/config commands.

## Installation

1. Download the binary for your preferred platform from the [releases](https://github.com/JakeTRogers/getRelease/releases) page
2. Extract the archive. It contains this readme, a copy of the Apache 2.0 license, and the getRelease binary.
3. Copy the binary to a directory in your `$PATH`. i.e. `/usr/local/bin`

## Usage

Install the latest release for a repository:

```bash
getRelease --owner sharkdp --repo bat
```

Install a specific tagged release:

```bash
getRelease --owner junegunn --repo fzf --tag 0.66.0
```

Resolve the repository from a GitHub URL instead of separate flags:

```bash
getRelease --url https://github.com/junegunn/fzf
```

Download an asset without installing it:

```bash
getRelease --owner sharkdp --repo fd --download-only
```

Install a single binary under a different name:

```bash
getRelease --owner sharkdp --repo bat --install-as bat-preview
```

List recent releases for a repository:

```bash
getRelease list --owner JakeTRogers --repo timeBuddy
```

List the assets for a specific release tag:

```bash
getRelease list --owner JakeTRogers --repo timeBuddy --tag v2.0.0
```

Preview an upgrade without changing anything:

```bash
getRelease upgrade timeBuddy --dry-run
```

Upgrade every installed package that still exists on disk:

```bash
getRelease upgrade --all
```

Inspect tracked installs:

```bash
getRelease history list
```

Prune history records for binaries that are no longer installed:

```bash
getRelease history prune
```

Show the effective configuration:

```bash
getRelease config show
```

Set preferred archive formats:

```bash
getRelease config set assetPreferences.formats '["tar.gz","zip"]'
```

The CLI stores configuration with Viper precedence rules:

- CLI flags
- Environment variables prefixed with `GETRELEASE_`
- `config.yaml`
- Built-in defaults

Common environment variable examples:

```bash
export GETRELEASE_INSTALLDIR="$HOME/.local/bin"
export GETRELEASE_ASSETPREFERENCES_FORMATS='["tar.gz","zip"]'
```

The main commands are:

- `getRelease`: download, extract, and install a release asset
- `getRelease list`: list recent releases or release assets
- `getRelease upgrade`: upgrade an installed package from recorded history
- `getRelease history`: inspect and maintain install history
- `getRelease config`: inspect and change configuration
- `getRelease version`: print version and platform information

### Shell Completion

`getRelease` uses Cobra's built-in `completion` command, so you can install shell completion with the standard Cobra flow:

```bash
# Bash
source <(getRelease completion bash)

# Zsh
source <(getRelease completion zsh)

# Fish
getRelease completion fish | source
```

The completion callbacks are extended to read local install history. That gives you history-backed suggestions for commands such as:

```bash
getRelease -o <TAB>
getRelease -o JakeTRogers -r <TAB>
getRelease upgrade <TAB>
getRelease upgrade -o <TAB>
```

`upgrade` suggestions are limited to binaries that are still installed on disk.

Generate completion scripts explicitly if you prefer to install them into your shell startup files:

```bash
getRelease completion bash
getRelease completion zsh
getRelease completion fish
```

## Notes

- Asset selection is automatic when there is exactly one match or one clearly preferred match for the current platform.
- When an archive contains multiple binaries, the CLI can install all of them or prompt you to choose one.
- Install history is what powers `upgrade`, owner and repo completion, and installed-target suggestions.

## Development

### Running Tests

```bash
go test ./... -v
```

### Coverage Report

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Pre-commit Hooks

This project uses [pre-commit](https://pre-commit.com/) hooks to ensure code quality. The hooks run:

- **go test**: Runs all tests with race detection
- **go test coverage**: Ensures cmd package maintains ≥85% coverage
- **golangci-lint**: Comprehensive Go linting
- **commitizen**: Enforces conventional commit messages

To install pre-commit hooks:

```bash
# Install pre-commit (if not already installed)
pip install pre-commit

# Install the git hooks
pre-commit install --hook-type pre-commit --hook-type commit-msg

# Run hooks manually on all files
pre-commit run --all-files
```

The hooks will automatically run before each commit. To skip hooks temporarily:

```bash
git commit --no-verify
```
