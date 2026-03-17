#!/usr/bin/env python3
"""summarize-findings.py – Reads campaign rubric JSON files and produces a
human-readable summary with pass/fail totals, known-gap status, and a
freeform findings section.

Usage:
    python3 product-tests/scripts/summarize-findings.py [OPTIONS] [RUBRIC_FILE ...]

    # Summarise every rubric in /tmp/campaign-findings/
    python3 product-tests/scripts/summarize-findings.py --dir /tmp/campaign-findings

    # Summarise a specific file
    python3 product-tests/scripts/summarize-findings.py run1.rubric.json run2.rubric.json

Options:
    --dir DIR       Read all *.rubric.json files from DIR.
    --schema PATH   Path to agent-rubric.schema.json for validation
                    (auto-detected from script location when omitted).
    --no-validate   Skip JSON schema validation.
    --format FMT    Output format: 'text' (default) or 'json'.
    --fail-fast     Exit 1 on the first failed campaign instead of summarising all.

Exit codes:
    0  All campaigns pass (or only known gaps remain).
    1  At least one campaign failed.
    2  Script / file error.
"""

from __future__ import annotations

import argparse
import json
import sys
import textwrap
from pathlib import Path


def _schema_path_from_script() -> Path | None:
    """Try to locate agent-rubric.schema.json relative to this script."""
    here = Path(__file__).resolve()
    # scripts/ -> product-tests/ -> testdata/expected/
    candidate = here.parent.parent / "testdata" / "expected" / "agent-rubric.schema.json"
    return candidate if candidate.exists() else None


def load_rubric(path: Path) -> dict:
    try:
        return json.loads(path.read_text())
    except Exception as exc:
        raise ValueError(f"Cannot read {path}: {exc}") from exc


def validate_rubric(rubric: dict, schema: dict) -> list[str]:
    """Return a list of validation error messages (empty = valid).

    Uses jsonschema if available; falls back to a minimal manual check.
    """
    try:
        from jsonschema import Draft202012Validator, ValidationError  # type: ignore

        validator = Draft202012Validator(schema)
        return [
            f"{'.'.join(str(p) for p in e.path) or '<root>'}: {e.message}"
            for e in sorted(validator.iter_errors(rubric), key=lambda e: list(e.path))
        ]
    except ImportError:
        # Minimal fallback validation.
        errors: list[str] = []
        for field in ("campaign", "runAt", "pass", "criteria", "findings"):
            if field not in rubric:
                errors.append(f"missing required field '{field}'")
        return errors


def format_rubric_text(rubric: dict, schema_errors: list[str]) -> str:
    lines: list[str] = []
    campaign = rubric.get("campaign", "<unknown>")
    run_at = rubric.get("runAt", "")
    overall = "PASS ✓" if rubric.get("pass") else "FAIL ✗"
    lines.append(f"┌─ {campaign}  [{run_at}]  {overall}")
    metadata = []
    for key in ("workstream", "capability", "environmentClass", "authPattern"):
        if rubric.get(key):
            metadata.append(f"{key}={rubric[key]}")
    if metadata:
        lines.append("│  Lane: " + "  ".join(metadata))

    if schema_errors:
        lines.append("│  ⚠ SCHEMA ERRORS:")
        for err in schema_errors:
            lines.append(f"│    - {err}")

    criteria: list[dict] = rubric.get("criteria") or []
    pass_count = sum(1 for c in criteria if c.get("pass", True))
    fail_count = len(criteria) - pass_count
    lines.append(f"│  Criteria: {pass_count} passed, {fail_count} failed (total {len(criteria)})")

    for c in criteria:
        if not c.get("pass", True):
            note = f"  ({c['note']})" if c.get("note") else ""
            lines.append(f"│    ✗ [{c.get('id', '?')}] {c.get('description', '')} "
                         f"— expected {c.get('expected', '?')!r}, got {c.get('actual', '?')!r}{note}")

    gaps: list[dict] = rubric.get("knownGaps") or []
    if gaps:
        still_fail = [g for g in gaps if g.get("stillFails", True)]
        now_pass = [g for g in gaps if not g.get("stillFails", True)]
        lines.append(f"│  Known gaps: {len(still_fail)} pending, {len(now_pass)} fixed")
        for g in now_pass:
            lines.append(f"│    ⚡ FIXED [{g.get('id', '?')}] {g.get('description', '')}")

    findings: list[str] = rubric.get("findings") or []
    if findings:
        lines.append("│  Findings:")
        for f in findings:
            lines.append(f"│    · {f}")

    artifact_paths: list[str] = rubric.get("artifactPaths") or []
    if artifact_paths:
        lines.append("│  Artifacts:")
        for path in artifact_paths:
            lines.append(f"│    · {path}")

    lines.append("└" + "─" * 60)
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Summarise campaign rubric JSON files.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=textwrap.dedent(__doc__ or ""),
    )
    parser.add_argument("files", nargs="*", metavar="RUBRIC_FILE", help="Rubric JSON files to summarise.")
    parser.add_argument("--dir", metavar="DIR", help="Directory containing *.rubric.json files.")
    parser.add_argument("--schema", metavar="PATH", help="Path to agent-rubric.schema.json.")
    parser.add_argument("--no-validate", action="store_true", help="Skip JSON schema validation.")
    parser.add_argument("--format", choices=["text", "json"], default="text", help="Output format.")
    parser.add_argument("--fail-fast", action="store_true", help="Exit on first failure.")
    args = parser.parse_args()

    # Collect input files.
    input_paths: list[Path] = [Path(f) for f in args.files]
    if args.dir:
        dir_path = Path(args.dir)
        input_paths.extend(sorted(dir_path.glob("*.rubric.json")))
        rubric_json = dir_path / "rubric.json"
        if rubric_json.exists():
            input_paths.append(rubric_json)

    if not input_paths:
        print("[error] No rubric files specified.  Use --dir or pass file paths.", file=sys.stderr)
        sys.exit(2)

    # Load schema for validation.
    schema: dict | None = None
    if not args.no_validate:
        schema_path = Path(args.schema) if args.schema else _schema_path_from_script()
        if schema_path and schema_path.exists():
            try:
                schema = json.loads(schema_path.read_text())
            except Exception as exc:
                print(f"[warn] Could not load schema: {exc}", file=sys.stderr)
        elif not args.schema:
            print("[warn] agent-rubric.schema.json not found; skipping validation.", file=sys.stderr)

    rubrics_out: list[dict] = []
    any_failed = False

    for path in input_paths:
        try:
            rub = load_rubric(path)
        except ValueError as exc:
            print(f"[error] {exc}", file=sys.stderr)
            sys.exit(2)

        schema_errors: list[str] = []
        if schema:
            schema_errors = validate_rubric(rub, schema)

        failed = not rub.get("pass", False) or bool(schema_errors)
        if failed:
            any_failed = True

        if args.format == "text":
            print(format_rubric_text(rub, schema_errors))
        else:
            rubrics_out.append({
                "file": str(path),
                "rubric": rub,
                "schemaErrors": schema_errors,
                "failed": failed,
            })

        if args.fail_fast and failed:
            if args.format == "json":
                print(json.dumps(rubrics_out, indent=2))
            sys.exit(1)

    if args.format == "json":
        print(json.dumps(rubrics_out, indent=2))

    if any_failed:
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()
