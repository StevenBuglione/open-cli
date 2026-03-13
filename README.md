# oas-cli-spec

Normative specification and JSON schemas for OAS-CLI.

## Contents

- `spec/core.md`: discovery, normalization, runtime, and governance requirements
- `spec/config.md`: `.cli.json` schema and scope precedence rules
- `spec/cli-profile.md`: command mapping profile and output requirements
- `spec/agent-profile.md`: skill manifest and agent safety semantics
- `schemas/*.schema.json`: machine-readable schemas for the published artifacts
- `examples/`: example documents validated in CI

## Validation

```bash
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
python scripts/validate_examples.py
```
