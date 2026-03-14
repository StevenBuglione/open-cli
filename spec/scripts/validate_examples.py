#!/usr/bin/env python3
import json
from pathlib import Path

from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parents[1]


def load_json(path: Path):
    return json.loads(path.read_text())


def validate(schema_name: str, example_name: str) -> None:
    schema = load_json(ROOT / "schemas" / schema_name)
    example = load_json(ROOT / "examples" / example_name)
    validator = Draft202012Validator(schema)
    errors = sorted(validator.iter_errors(example), key=lambda error: list(error.path))
    if errors:
        lines = [f"{example_name} failed {schema_name} validation:"]
        for error in errors:
            path = ".".join(str(part) for part in error.path) or "<root>"
            lines.append(f"  - {path}: {error.message}")
        raise SystemExit("\n".join(lines))


def validate_rejects(schema_name: str, example_name: str) -> None:
    schema = load_json(ROOT / "schemas" / schema_name)
    example = load_json(ROOT / "examples" / example_name)
    validator = Draft202012Validator(schema)
    errors = sorted(validator.iter_errors(example), key=lambda error: list(error.path))
    if not errors:
        raise SystemExit(f"{example_name} unexpectedly passed {schema_name} validation")


def main() -> None:
    validate("cli.schema.json", "project.cli.json")
    validate("skill-manifest.schema.json", "skill-manifest.json")
    validate("ntc.schema.json", "ntc.json")
    validate("compatibility-matrix.schema.json", "compatibility-matrix.json")
    validate_rejects("cli.schema.json", "invalid-openapi-transport.cli.json")
    validate_rejects("cli.schema.json", "invalid-openapi-oauth.cli.json")
    validate_rejects("cli.schema.json", "invalid-mcp-uri.cli.json")
    print("validated 4 example documents and 3 negative schema checks")


if __name__ == "__main__":
    main()
