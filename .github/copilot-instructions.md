# Copilot instructions

This repository hosts Model Context Protocol servers. Each server lives in its
own top-level directory and is self-contained. `attic/` is dead code kept for
reference: do not read it for patterns, and do not change it.

## Layout

- `prolog/` — `mcprolog`, a Go module (`github.com/smxlong/mcp/prolog`).
  - `cmd/mcprolog/` — flags, transports, process lifecycle.
  - `internal/mcprolog/` — the server: knowledge base, SWI-Prolog runner, tools.
  - `internal/mcprolog/driver.pl` — the Prolog side, embedded with `go:embed`.

## Working here

- Build and test from the server's directory, not the repository root.
- `make test-docker` is the reliable way to run the tests: they drive a real
  `swipl`, and a minimal SWI-Prolog install (for example Debian's
  `swi-prolog-core`) lacks `library(time)` and `library(http/json)`.
- `make build`, `make lint`, `make image` cover the rest.

## Code

- Standard Go style. Run `gofmt` and `go vet`; `golangci-lint` gates CI.
- Prefer the standard library. Add a dependency only when it earns its place.
- Comments explain why, not what. If a line looks odd, say what would go wrong
  without it; otherwise leave it uncommented.
- Keep it small. Before adding a helper, check whether an existing one covers
  the case.
- Tests use `testify`: `require` for preconditions, `assert` for expectations.
  Test names describe the behaviour being pinned down.
- Everything the agent sends is untrusted. Preserve the existing defences:
  inputs reach `swipl` as files rather than as command text, goals go through
  `library(sandbox)`, directives are filtered separately, and every run is
  bounded by a time limit, a stack limit and a fresh process.

## Commits and releases

- [Conventional Commits](https://www.conventionalcommits.org), scoped by
  directory: `feat(prolog):`, `fix(prolog):`, `chore(ci):`.
- One logical change per commit; keep unrelated cleanups separate.
- Releases come from tags of the form `prolog/vX.Y.Z`. Update that server's
  `CHANGELOG.md` in the commit being tagged.
- Do not describe how the code was written, or by whom, in comments, commit
  messages or documentation.
