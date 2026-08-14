# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's
[security advisory form](https://github.com/smxlong/mcp/security/advisories/new)
rather than in a public issue. I will confirm receipt and let you know when a
fix is released.

## Supported versions

The most recent release of each server is supported. Fixes are not backported.

## Threat model

These servers execute instructions on behalf of a language model, so treat
everything an agent sends as untrusted input.

`mcprolog` runs agent-supplied Prolog. It screens goals with
`library(sandbox)`, restricts the directives a unit may contain, bounds every
goal with a time limit and a stack limit, and runs each one in a short-lived
subprocess with a minimal environment. That is a meaningful barrier, not a
guarantee: run it as an unprivileged user, and in a container if the knowledge
base is not fully trusted.
