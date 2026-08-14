# mcprolog

An MCP server that gives an agent a persistent Prolog knowledge base and the
ability to reason over it: assert facts and rules, choose which of them are in
scope, then query, prove and explain goals.

Facts and rules live in **units** — named groups of clauses. Only units that
are **in scope** are visible to a query, so an agent can reason under different
sets of assumptions by loading and unloading units rather than by rewriting
them.

## Thanks to SWI-Prolog

All of the reasoning here is done by [SWI-Prolog](https://www.swi-prolog.org/).
This server is a thin shell around it: it keeps the knowledge base, decides what
is in scope, and renders answers. Everything that makes those answers correct —
the engine, `library(sandbox)`, `library(time)`, the meta-interpreter hooks the
proof trees are built from — comes from three decades of work by Jan Wielemaker
and the SWI-Prolog community, released under a permissive licence. Thank you.

SWI-Prolog is not affiliated with this project. Bugs here are mine.

## Install

Container image, which includes SWI-Prolog:

```sh
docker run --rm -i -v mcprolog-state:/home/prolog/state ghcr.io/smxlong/mcprolog
```

From source, if you already have SWI-Prolog installed:

```sh
go install github.com/smxlong/mcp/prolog/cmd/mcprolog@latest
```

Binaries for each release are attached to the
[GitHub release](https://github.com/smxlong/mcp/releases).

A full SWI-Prolog installation is required. Minimal packages such as Debian's
`swi-prolog-core` omit `library(time)` and `library(http/json)`, which the
driver depends on; `swi-prolog` or the official image has everything.

## Configure a client

stdio, the usual case:

```json
{
  "servers": {
    "prolog": {
      "command": "docker",
      "args": ["run", "--rm", "-i", "-v", "mcprolog-state:/home/prolog/state", "ghcr.io/smxlong/mcprolog"]
    }
  }
}
```

Streamable HTTP, for one server shared by several clients:

```sh
docker run --rm -p 8080:8080 -v mcprolog-state:/home/prolog/state ghcr.io/smxlong/mcprolog -http :8080
```

## Tools

| Tool | Purpose |
| --- | --- |
| `prolog_list_units` | List units and the current scope |
| `prolog_create_unit` | Create a named unit |
| `prolog_delete_unit` | Delete a unit |
| `prolog_show_unit` | Show a unit's source with clause positions |
| `prolog_set_unit_source` | Rewrite a unit wholesale |
| `prolog_assert` | Add clauses to a unit |
| `prolog_retract` | Remove clauses whose head unifies with a pattern |
| `prolog_find_clauses` | Search all units for clauses matching a pattern |
| `prolog_check` | Load and report syntax errors, warnings and predicates |
| `prolog_load_unit` | Bring a unit into query scope |
| `prolog_unload_unit` | Take a unit out of query scope |
| `prolog_set_scope` | Replace the whole scope at once |
| `prolog_query` | Enumerate solutions with variable bindings |
| `prolog_prove` | Yes/no plus the first solution's bindings |
| `prolog_explain` | Return a proof tree for a goal |

## Options

| Flag | Default | Meaning |
| --- | --- | --- |
| `-http` | *(stdio)* | Serve Streamable HTTP on this address |
| `-state` | `prolog-kb.json` | Knowledge base file; empty keeps state in memory |
| `-swipl` | `swipl` | Path to the SWI-Prolog executable |
| `-timeout` | `10s` | Default time limit for a goal |
| `-stack-limit` | `512m` | SWI-Prolog stack limit per query process |
| `-max-solutions` | `200` | Hard cap on solutions per query |
| `-sandbox` | `true` | Reject goals `library(sandbox)` considers unsafe |
| `-allow-directives` | `false` | Permit arbitrary `:- Goal.` directives in unit source |
| `-serialize` | `true` | Run tool calls one at a time |
| `-version` | | Print the version and exit |

`MCPROLOG_STATE` and `MCPROLOG_SWIPL` provide defaults for `-state` and
`-swipl`.

## Design

`driver.pl` is embedded in the binary with `go:embed` and executed as a
short-lived `swipl` subprocess per request. A fresh process per query is slower
than a persistent toplevel, but it makes each query hermetic: a goal that
corrupts the database, exhausts the stack, or wedges the engine cannot affect
the next one. Durable state lives in Go, not in the Prolog process.

Inputs reach `swipl` as files and plain argv scalars, never as interpolated
command text, so agent-supplied Prolog cannot escape into a shell. Goals are
additionally screened by `library(sandbox)` and bounded by both an in-Prolog
time limit and a process-level deadline.

Directives (`:- Goal.`) are filtered separately from goals, because they run at
load time before `library(sandbox)` sees anything; only declarations such as
`dynamic` and `discontiguous` are permitted by default.

### Concurrency

Tool calls are serialised by default (`-serialize`). The MCP SDK executes every
request except `initialize` concurrently, and several tools here are
read-modify-write: `prolog_retract`, for instance, asks SWI-Prolog which clause
*positions* match and then deletes those positions. Overlapping calls apply
positions computed against a different version of the unit and delete the wrong
clauses — reproducibly, not occasionally.

Serialisation guarantees mutual exclusion, not arrival order. The SDK releases
each request for concurrent execution before middleware runs, so a client that
needs one call to observe another's effect must still wait for the first
response before sending the second. Mutating tools return the resulting state
(`scope`, `remaining`, the written clauses) so that a follow-up read is usually
unnecessary.

### Undefined predicates

A rule may refer to a predicate defined in a unit that is not currently in
scope. Such a reference simply fails, in keeping with the closed-world
assumption, rather than raising an error — so loading or unloading a unit
changes conclusions instead of breaking queries.

A predicate named *directly* in a goal must still exist, so typos are reported
rather than silently failing. `library(sandbox)` distinguishes the two cases by
reporting the chain of goals through which it reached the missing predicate.

## Development

```sh
make build         # binary in bin/
make test-docker   # tests, in an image with a full SWI-Prolog
make lint
make image
```

`smoke.sh` drives the built image over real stdio JSON-RPC.

## License

[MIT](../LICENSE).
