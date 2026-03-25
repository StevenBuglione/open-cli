# oas-cli-conformance

Language-neutral fixtures and expected outputs for validating Open CLI implementations.

> **Monorepo note:** This directory is a first-class subproject of [open-cli](../README.md). The runner uses `spec/schemas/` (the sibling subproject) as the default schema root, so you do not need to check out a separate repository.

## Contents

- `fixtures/`: discovery, OpenAPI, overlay, workflow, and config inputs
- `expected/`: expected normalized outputs
- `compatibility-matrix.json`: machine-readable suite/spec/implementation compatibility publication
- `COMPATIBILITY.md`: human-readable compatibility summary
- `scripts/run_conformance.py`: fixture validation and optional output comparison

## Usage

From this directory:

```bash
python3 -m venv .venv
. .venv/bin/activate
python -m pip install -r requirements.txt
python scripts/run_conformance.py --schema-root ../spec/schemas
python scripts/run_conformance.py --schema-root ../spec/schemas --candidate /path/to/generated.ntc.json
```

Or from the repository root using the convenience targets:

```bash
make verify-conformance   # runs with spec/schemas/ automatically
make verify-all           # Go + spec + conformance together
```

## MCP and OAuth fixtures

The suite now carries representative config fixtures for:

- native MCP sources using `stdio`
- `.mcp.json`-style `mcpServers` compatibility input
- authenticated `streamable-http` MCP sources with `headerSecrets`
- OpenAPI-facing OAuth runtime config shapes published through the shared schema

Those fixtures are validated against the published `cli.schema.json` so schema drift shows up in conformance before implementation repos diverge silently.
