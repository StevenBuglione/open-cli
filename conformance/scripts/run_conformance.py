#!/usr/bin/env python3
import argparse
import json
import os
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parents[1]


def load_json(path: Path):
    return json.loads(path.read_text())


def resolve_schema_root(explicit_root: Path | None = None) -> Path:
    candidates = []
    if explicit_root is not None:
        candidates.append(explicit_root)

    env_root = os.getenv("OASCLI_SCHEMA_ROOT")
    if env_root:
        candidates.append(Path(env_root))

    # Monorepo layout: spec/ lives at the same level as conformance/
    candidates.append(ROOT.parent / "spec" / "schemas")

    # Legacy fallback: sibling oas-cli-spec repository
    candidates.append(ROOT.parent / "oas-cli-spec" / "schemas")

    for candidate in candidates:
        if candidate.exists() and candidate.is_dir():
            return candidate

    searched = ", ".join(str(candidate) for candidate in candidates)
    raise FileNotFoundError(f"schema root not found; searched: {searched}")


def validate_expected_ntc(schema_root: Path) -> None:
    schema = load_json(schema_root / "ntc.schema.json")
    expected = load_json(ROOT / "expected" / "tickets.ntc.json")
    validator = Draft202012Validator(schema)
    errors = sorted(validator.iter_errors(expected), key=lambda error: list(error.path))
    if errors:
        raise SystemExit("\n".join(
            ["expected/tickets.ntc.json failed schema validation:"]
            + [f"  - {'.'.join(str(part) for part in error.path) or '<root>'}: {error.message}" for error in errors]
        ))


def validate_compatibility_matrix(schema_root: Path) -> None:
    schema = load_json(schema_root / "compatibility-matrix.schema.json")
    matrix = load_json(ROOT / "compatibility-matrix.json")
    validator = Draft202012Validator(schema)
    errors = sorted(validator.iter_errors(matrix), key=lambda error: list(error.path))
    if errors:
        raise SystemExit("\n".join(
            ["compatibility-matrix.json failed schema validation:"]
            + [f"  - {'.'.join(str(part) for part in error.path) or '<root>'}: {error.message}" for error in errors]
        ))


def validate_fixture_shapes(schema_root: Path) -> None:
    cli_schema = load_json(schema_root / "cli.schema.json")
    cli_validator = Draft202012Validator(cli_schema)
    load_json(ROOT / "fixtures" / "discovery" / "api-catalog.linkset.json")
    load_json(ROOT / "fixtures" / "discovery" / "service-meta.linkset.json")
    yaml.safe_load((ROOT / "fixtures" / "openapi" / "tickets.openapi.yaml").read_text())
    yaml.safe_load((ROOT / "fixtures" / "overlays" / "tickets.overlay.yaml").read_text())
    yaml.safe_load((ROOT / "fixtures" / "workflows" / "tickets.arazzo.yaml").read_text())
    for config_path in sorted((ROOT / "fixtures" / "config").glob("*.cli.json")):
        document = load_json(config_path)
        errors = sorted(cli_validator.iter_errors(document), key=lambda error: list(error.path))
        if errors:
            raise SystemExit("\n".join(
                [f"{config_path.relative_to(ROOT)} failed schema validation:"]
                + [f"  - {'.'.join(str(part) for part in error.path) or '<root>'}: {error.message}" for error in errors]
            ))


def validate_docs_linkage() -> None:
    readme = (ROOT / "README.md").read_text()
    compatibility_doc = ROOT / "COMPATIBILITY.md"
    if not compatibility_doc.exists():
        raise SystemExit("COMPATIBILITY.md is missing")
    if "COMPATIBILITY.md" not in readme and "compatibility-matrix.json" not in readme:
        raise SystemExit("README.md must mention the published compatibility matrix")


def compare_candidate(candidate_path: Path) -> None:
    candidate = normalize_ntc(load_json(candidate_path))
    expected = normalize_ntc(load_json(ROOT / "expected" / "tickets.ntc.json"))
    if candidate != expected:
        raise SystemExit(f"candidate output {candidate_path} does not match expected/tickets.ntc.json")


def normalize_ntc(document: dict) -> dict:
    normalized = json.loads(json.dumps(document))
    normalized.pop("generatedAt", None)
    normalized.pop("sourceFingerprint", None)
    for source in normalized.get("sources", []):
        provenance = source.get("provenance", {})
        provenance.pop("at", None)
    return normalized


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate", type=Path)
    parser.add_argument("--schema-root", type=Path)
    args = parser.parse_args()

    schema_root = resolve_schema_root(args.schema_root)
    validate_fixture_shapes(schema_root)
    validate_expected_ntc(schema_root)
    validate_compatibility_matrix(schema_root)
    validate_docs_linkage()
    if args.candidate:
        compare_candidate(args.candidate)
    print("conformance fixture validation passed")


if __name__ == "__main__":
    main()
