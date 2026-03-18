# oas-cli-spec

Normative specification and JSON schemas for Open CLI.

> **Monorepo note:** This directory is a first-class subproject of [open-cli](../README.md). It is the single source of truth for the Open CLI public contract. The Go implementation and the conformance suite both consume the schemas from this directory.

## Contents

- `spec/core.md`: discovery, normalization, runtime, and governance requirements
- `spec/config.md`: `.cli.json` schema and scope precedence rules
- `spec/cli-profile.md`: command mapping profile and output requirements
- `spec/agent-profile.md`: skill manifest and agent safety semantics
- `schemas/*.schema.json`: machine-readable schemas for the published artifacts, including the compatibility matrix
- `examples/`: example documents validated in CI

## Validation

From this directory:

```bash
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
python scripts/validate_examples.py
```

Or from the repository root using the convenience target:

```bash
make verify-spec
```
