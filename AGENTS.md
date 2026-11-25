# AGENTS.md

## Conventions

- A cancelled prompt returns `selector.ErrCancelled` up the stack; `reportError` in `cmd/root.go` maps it to exit status 2 and every other error to 1.
- `cmd` tests fake the GitHub client, prompts, installer, and config by swapping the package-level seams in `cmd/deps.go` through `useTestCommandDeps`. Those tests mutate globals, so they run serially. Other tests are table-driven with `t.Parallel()`, using `httptest.Server` for GitHub API calls.
- Pre-commit hooks (`.pre-commit-config.yaml`) gate commits on golangci-lint, `go test -race`, 80% total coverage, and Conventional Commit messages.

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues for JakeTRogers/getRelease (via `gh`). See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the five default triage labels: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one root `GLOSSARY.md` + `docs/adr/`. See `docs/agents/domain.md`.
