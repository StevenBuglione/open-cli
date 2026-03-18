# Compatibility Matrix

This document publishes the current compatibility status for the reference implementation.

## Current Release

| Suite | Spec | Implementation | Overall | HTTP caching | Refresh | Observability hooks | MCP transports | OAuth runtime | Matrix publication |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `1.0.0` | `0.1.0` | `StevenBuglione/open-cli@main` | partial | passing | passing | passing | partial | partial | passing |

The machine-readable source of truth for this table is [`compatibility-matrix.json`](./compatibility-matrix.json).

Feature rows in this table are backed by two inputs:

- schema and fixture validation in `oas-cli-conformance`
- implementation verification in `open-cli` for runtime transport and auth behavior

`MCP transports` and `OAuth runtime` remain `partial` here until that runtime evidence is mechanically enforced by this repository's own validation flow.
