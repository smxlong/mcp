# mcp

Model Context Protocol servers.

This repository is where my MCP work lives. Each server is a self-contained
directory with its own build, tests and release cycle; nothing is shared
between them except the conventions described below.

## Servers

| Server | Directory | What it does |
| --- | --- | --- |
| [mcprolog](prolog/) | `prolog/` | A persistent SWI-Prolog knowledge base an agent can query, prove and explain goals against |

`attic/` holds earlier experiments. It is kept for reference, is not built or
tested, and should not be used.

## Conventions

- One directory per server, self-contained, with its own README.
- Go servers are separate modules, so each has its own dependencies and version.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org),
  scoped by directory: `feat(prolog): ...`.
- Releases are cut by pushing a tag of the form `<server>/vX.Y.Z`, which builds
  binaries and container images for that server alone.

## Contributing

Bug reports and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md)
for how to build and test, and [SECURITY.md](SECURITY.md) for how to report a
vulnerability.

## License

[MIT](LICENSE).
