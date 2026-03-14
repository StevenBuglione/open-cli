# oas-cli-conformance

Language-neutral fixtures and expected outputs for validating OAS-CLI implementations.

## Contents

- `fixtures/`: discovery, OpenAPI, overlay, workflow, and config inputs
- `expected/`: expected normalized outputs
- `compatibility-matrix.json`: machine-readable suite/spec/implementation compatibility publication
- `COMPATIBILITY.md`: human-readable compatibility summary
- `scripts/run_conformance.py`: fixture validation and optional output comparison

## Usage

```bash
python3 -m pip install -r requirements.txt
python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas
python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas --candidate /path/to/generated.ntc.json
```

The runner validates expected artifacts against the published schemas from `oas-cli-spec`, so standalone CI jobs must either check out that repository or provide an equivalent schema directory via `--schema-root` or `OASCLI_SCHEMA_ROOT`.

The same runner also validates `compatibility-matrix.json` against the published compatibility matrix schema and ensures the published matrix is linked from the repository documentation.

The published matrix summarizes combined evidence: schema/conformance checks in this repository plus implementation verification from the referenced runtime repository.

Until runtime verification is enforced directly by this repository, runtime-heavy feature rows such as MCP transport behavior and OAuth execution are published as `partial`.

## MCP and OAuth fixtures

The suite now carries representative config fixtures for:

- native MCP sources using `stdio`
- `.mcp.json`-style `mcpServers` compatibility input
- authenticated `streamable-http` MCP sources with `headerSecrets`
- OpenAPI-facing OAuth runtime config shapes published through the shared schema

Those fixtures are validated against the published `cli.schema.json` so schema drift shows up in conformance before implementation repos diverge silently.
