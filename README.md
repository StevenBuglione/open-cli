# oas-cli-conformance

Language-neutral fixtures and expected outputs for validating OAS-CLI implementations.

## Contents

- `fixtures/`: discovery, OpenAPI, overlay, workflow, and config inputs
- `expected/`: expected normalized outputs
- `scripts/run_conformance.py`: fixture validation and optional output comparison

## Usage

```bash
python3 -m pip install -r requirements.txt
python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas
python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas --candidate /path/to/generated.ntc.json
```

The runner validates expected artifacts against the published schemas from `oas-cli-spec`, so standalone CI jobs must either check out that repository or provide an equivalent schema directory via `--schema-root` or `OASCLI_SCHEMA_ROOT`.
