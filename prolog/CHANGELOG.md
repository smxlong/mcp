# Changelog

All notable changes to mcprolog are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-08-13

First release.

- Persistent knowledge base of Prolog clauses, organised into units, with a
  query scope that decides which units a goal sees.
- 15 tools covering unit management, scope control, querying, proving and
  proof-tree explanation.
- stdio and Streamable HTTP transports.
- Goals screened by `library(sandbox)`, directives restricted to declarations,
  and every run bounded by a time limit, a stack limit and a fresh process.

[Unreleased]: https://github.com/smxlong/mcp/compare/prolog/v0.1.0...HEAD
[0.1.0]: https://github.com/smxlong/mcp/releases/tag/prolog/v0.1.0
