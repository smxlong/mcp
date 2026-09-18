# Contributing

Thanks for taking an interest. Issues and pull requests are welcome.

## Working on a server

Each server is self-contained. Build and test from its own directory:

```sh
cd prolog
make build
make test-docker   # tests need a full SWI-Prolog; this runs them in one
```

`make test` runs the same tests directly, and needs SWI-Prolog installed with
its standard libraries (Debian's `swi-prolog-core` alone is not enough).

For ScoreTex, run `cd scoretex`, then `make test-docker` and `make validate`.
The validation script demonstrates the complete supported API through stdio MCP.

## Pull requests

- Keep changes focused; unrelated cleanups belong in their own commit.
- Add or adjust tests for behaviour you change.
- Run `gofmt`, `go vet` and the tests before pushing. CI runs the same checks
  plus `golangci-lint`.
- Write [Conventional Commits](https://www.conventionalcommits.org) with the
  server as the scope, for example `fix(prolog): keep clause positions stable`.

## Releasing

Releases are per server and are cut from a tag:

```sh
git tag prolog/v0.1.0
git push origin prolog/v0.1.0
```

The release workflow builds the binaries and container image, attaches
checksums, and publishes a GitHub release. Update that server's `CHANGELOG.md`
in the commit you tag.

## Agentic skill

Agents should read:

https://raw.githubusercontent.com/smxlong/skills/refs/heads/main/coding/mcp-go/SKILL.md
