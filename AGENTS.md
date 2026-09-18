# Copilot instructions

This repository hosts Model Context Protocol servers. Each server lives in its
own top-level directory and is self-contained.

## Layout

- `prolog/` — `mcprolog`, a Go module (`github.com/smxlong/mcp/prolog`).
  - `cmd/mcprolog/` — flags, transports, process lifecycle.
  - `internal/mcprolog/` — the server: knowledge base, SWI-Prolog runner, tools.
  - `internal/mcprolog/driver.pl` — the Prolog side, embedded with `go:embed`.

## Starting

Read README.md and CONTRIBUTING.md

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

## Cohesive framework and family, not dumping ground

The MCP servers here are meant to share a common core. That means the business
logic is pinned between two shared aspects: the framework, which provides the
harness and body shape of the server in an opinionated way, and the libraries,
which provide the opinionated and valuable mixed-in behaviors that are
shared across different servers. The logic of a given server is held elegantly
in a harness in the middle. That's the vision.

## Commits and releases

- [Conventional Commits](https://www.conventionalcommits.org), scoped by
  directory: `feat(prolog):`, `fix(prolog):`, `chore(ci):`.
- One logical change per commit; keep unrelated cleanups separate.
- Releases come from tags of the form `prolog/vX.Y.Z`. Update that server's
  `CHANGELOG.md` in the commit being tagged.
- Do not describe how the code was written, or by whom, in comments, commit
  messages or documentation.

## The smxlong/skills/coding/mcp-go skill

At https://github.com/smxlong/skills is a directory of skills. The `coding/mcp-go`
skill gives guidance on proper development of MCP servers in Go (such as is done
in this repo itself). Consult and maintain this skill as the source of truth for
MCP development. You own the text under `coding/mcp-go` in that repo. Push
branches and open PRs against it as needed. The skill is public, so you're
helping the world when you improve it.

Quickly read the skill with `gh api repos/smxlong/skills/contents/coding/mcp-go/SKILL.md --jq '.content' | base64 -d`.
