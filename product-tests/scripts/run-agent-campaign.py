#!/usr/bin/env python3
"""run-agent-campaign.py – Orchestrates one or more agent/operator campaign runs.

Usage:
    python3 product-tests/scripts/run-agent-campaign.py [OPTIONS]

Options:
    --campaign PATTERN   Run only campaigns matching PATTERN (default: all).
                         Pattern is matched as a substring of the test name.
    --output-dir DIR     Directory to write rubric JSON files (default: /tmp/campaign-findings).
    --timeout SECONDS    Per-test timeout passed to `go test` (default: 120).
    --verbose            Pass -v to `go test`.
    --root DIR           Repository root dir (default: auto-detected from this script's location).

Exit codes:
    0  All campaign criteria passed (or only known gaps remain).
    1  At least one campaign criterion failed.
    2  Script error (could not run tests, bad arguments, etc.).

The script:
1. Runs `go test ./product-tests/tests/... -run <PATTERN>` and captures output.
2. Parses rubric JSON blobs emitted inside test log lines (lines containing
   "=== Campaign Rubric").
3. Writes each rubric to --output-dir as <campaign>-<runAt>.rubric.json.
4. Prints a compact summary table and exits with the appropriate code.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import textwrap
from pathlib import Path

import yaml


def repo_root_from_script() -> Path:
    """Walk up from this script to find the go.mod root."""
    here = Path(__file__).resolve()
    for parent in [here.parent, here.parent.parent, here.parent.parent.parent]:
        if (parent / "go.mod").exists():
            return parent
    raise FileNotFoundError("go.mod not found from script location")


def run_go_tests(
    root: Path,
    pattern: str,
    timeout: int,
    verbose: bool,
) -> tuple[int, str]:
    """Run `go test` and return (returncode, combined_output)."""
    cmd = [
        "go",
        "test",
        "./product-tests/tests/...",
        f"-run={pattern}",
        f"-timeout={timeout}s",
        "-count=1",
        "-v",
    ]

    proc = subprocess.run(
        cmd,
        cwd=root,
        capture_output=True,
        text=True,
    )
    combined = proc.stdout + proc.stderr
    return proc.returncode, combined


_RUBRIC_HEADER_RE = re.compile(r"=== Campaign Rubric \[([^\]]+)\] ===")


def extract_rubrics(output: str) -> list[dict]:
    """Parse rubric JSON blobs from `go test -v` output.

    The helpers.FindingsRecorder.MustEmitToTest logs:
        === Campaign Rubric [<name>] ===
        <json blob>

    We extract the JSON by consuming lines after the header until the
    blob is complete (balanced braces).
    """
    rubrics: list[dict] = []
    lines = output.splitlines()
    i = 0
    while i < len(lines):
        m = _RUBRIC_HEADER_RE.search(lines[i])
        if m:
            # Collect JSON lines starting from the next line.
            json_lines: list[str] = []
            depth = 0
            i += 1
            while i < len(lines):
                raw = lines[i]
                # go test -v prefixes log lines with a timestamp + spacing;
                # strip the common "    " or "        " indent.
                stripped = raw.lstrip()
                json_lines.append(stripped)
                depth += stripped.count("{") - stripped.count("}")
                i += 1
                if depth <= 0 and json_lines:
                    break
            blob = "\n".join(json_lines)
            try:
                rubrics.append(json.loads(blob))
            except json.JSONDecodeError:
                print(
                    f"[warn] Could not parse rubric JSON for campaign '{m.group(1)}'",
                    file=sys.stderr,
                )
        else:
            i += 1
    return rubrics


def write_rubric(rubric: dict, output_dir: Path) -> Path:
    """Persist a rubric dict to output_dir and return the path."""
    output_dir.mkdir(parents=True, exist_ok=True)
    name = rubric.get("campaign", "unknown")
    run_at = rubric.get("runAt", "unknown").replace(":", "-")
    fname = f"{name}-{run_at}.rubric.json"
    path = output_dir / fname
    path.write_text(json.dumps(rubric, indent=2))
    return path


def write_lane_rubric(rubric: dict, lane: dict, output_dir: Path, transcript_path: Path) -> Path:
    """Persist one rubric using stable fleet lane filenames."""
    output_dir.mkdir(parents=True, exist_ok=True)
    rubric = dict(rubric)
    rubric["workstream"] = lane["workstream"]
    rubric["capability"] = lane["capability"]
    rubric["environmentClass"] = lane["environmentClass"]
    rubric["authPattern"] = lane["authPattern"]
    rubric["artifactPaths"] = sorted(set([
        "rubric.json",
        transcript_path.name,
    ]))
    path = output_dir / "rubric.json"
    path.write_text(json.dumps(rubric, indent=2))
    return path


def write_transcript(output: str, output_dir: Path) -> Path:
    output_dir.mkdir(parents=True, exist_ok=True)
    path = output_dir / "transcript.log"
    path.write_text(output)
    return path


def load_capability_matrix(path: Path) -> list[dict]:
    payload = yaml.safe_load(path.read_text()) or {}
    lanes = payload.get("lanes") or []
    if not lanes:
        raise ValueError(f"capability matrix {path} has no lanes")
    for lane in lanes:
        for field in ("id", "workstream", "capability", "environmentClass", "authPattern", "goTestPattern"):
            if not lane.get(field):
                raise ValueError(f"lane missing required field {field!r}: {lane}")
    return lanes


def select_lanes(lanes: list[dict], requested: list[str] | None) -> list[dict]:
    if not requested:
        return lanes
    by_id = {lane["id"]: lane for lane in lanes}
    missing = [lane_id for lane_id in requested if lane_id not in by_id]
    if missing:
        raise ValueError(f"unknown lane id(s): {', '.join(missing)}")
    return [by_id[lane_id] for lane_id in requested]


def exact_test_name(pattern: str) -> str | None:
    match = re.fullmatch(r"\^([A-Za-z0-9_]+)\$$", pattern)
    if not match:
        return None
    return match.group(1)


def selected_lane_was_skipped(output: str, go_test_pattern: str) -> bool:
    test_name = exact_test_name(go_test_pattern)
    if not test_name:
        return False
    return f"--- SKIP: {test_name} " in output


def print_summary(rubrics: list[dict], go_returncode: int) -> int:
    """Print a human-readable summary table.  Returns the suggested exit code."""
    if not rubrics:
        print("[warn] No rubric JSON found in test output.")
        # Honour the go test exit code even without parsed rubrics.
        return go_returncode if go_returncode != 0 else 0

    total_criteria = 0
    failed_criteria = 0
    total_gaps = 0
    gaps_fixed = 0

    rows: list[tuple[str, str, str, int, int, int]] = []  # (campaign, capability, pass, total, fail, gaps)

    for rub in rubrics:
        criteria: list[dict] = rub.get("criteria") or []
        gaps: list[dict] = rub.get("knownGaps") or []
        n_total = len(criteria)
        n_fail = sum(1 for c in criteria if not c.get("pass", True))
        n_gaps = len(gaps)
        n_fixed = sum(1 for g in gaps if not g.get("stillFails", True))
        total_criteria += n_total
        failed_criteria += n_fail
        total_gaps += n_gaps
        gaps_fixed += n_fixed
        status = "PASS" if rub.get("pass", False) else "FAIL"
        rows.append((rub.get("campaign", "?"), rub.get("capability", "-"), status, n_total, n_fail, n_gaps))

    print()
    print("=" * 72)
    print("  CAMPAIGN FINDINGS SUMMARY")
    print("=" * 72)
    col_w = 28
    cap_w = 18
    print(f"  {'CAMPAIGN':<{col_w}} {'CAPABILITY':<{cap_w}} {'STATUS':<6}  CRIT  FAIL  GAPS")
    print("  " + "-" * 68)
    for campaign, capability, status, n_total, n_fail, n_gaps in rows:
        marker = "✓" if status == "PASS" else "✗"
        print(f"  {marker} {campaign:<{col_w - 2}} {capability:<{cap_w}} {status:<6}  {n_total:>4}  {n_fail:>4}  {n_gaps:>4}")
    print("  " + "-" * 68)
    print(f"  {'TOTALS':<{col_w}} {'':<{cap_w}} {'':6}  {total_criteria:>4}  {failed_criteria:>4}  {total_gaps:>4}")
    if gaps_fixed:
        print(f"\n  ⚡ {gaps_fixed} known gap(s) now PASSING — consider promoting to assertions.")
    print()

    if failed_criteria > 0:
        print("  ✗ Campaign run FAILED — see rubric files for details.")
        return 1
    if go_returncode != 0:
        print("  ✗ go test reported a failure even though rubric criteria passed.")
        return 1
    print("  ✓ All criteria passed.")
    return 0


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Run agent/operator campaign tests and capture structured findings.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=textwrap.dedent(__doc__ or ""),
    )
    parser.add_argument(
        "--campaign",
        default="TestCampaign",
        metavar="PATTERN",
        help="go test -run pattern (default: TestCampaign)",
    )
    parser.add_argument(
        "--matrix",
        default=None,
        metavar="PATH",
        help="Capability matrix YAML file describing executable fleet lanes",
    )
    parser.add_argument(
        "--lane",
        action="append",
        dest="lanes",
        default=None,
        metavar="ID",
        help="Specific lane ID to run from --matrix (repeatable)",
    )
    parser.add_argument(
        "--output-dir",
        default="/tmp/campaign-findings",
        metavar="DIR",
        help="Directory for rubric JSON output",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=120,
        metavar="SECONDS",
        help="Per-test timeout in seconds",
    )
    parser.add_argument("--verbose", action="store_true", help="Pass -v to go test")
    parser.add_argument(
        "--root",
        default=None,
        metavar="DIR",
        help="Repository root (auto-detected if omitted)",
    )
    args = parser.parse_args()

    try:
        root = Path(args.root) if args.root else repo_root_from_script()
    except FileNotFoundError as exc:
        print(f"[error] {exc}", file=sys.stderr)
        sys.exit(2)

    output_dir = Path(args.output_dir)

    print(f"[run-agent-campaign] root={root}")
    all_rubrics: list[dict] = []
    raw_failures: list[str] = []
    overall_go_returncode = 0

    if args.matrix:
        matrix_path = Path(args.matrix)
        if not matrix_path.is_absolute():
            matrix_path = root / matrix_path
        try:
            lanes = select_lanes(load_capability_matrix(matrix_path), args.lanes)
        except ValueError as exc:
            print(f"[error] {exc}", file=sys.stderr)
            sys.exit(2)

        lane_validation_failures: list[str] = []
        for lane in lanes:
            lane_output_dir = output_dir / lane["id"]
            print(f"[run-agent-campaign] lane={lane['id']!r} pattern={lane['goTestPattern']!r} timeout={args.timeout}s")
            returncode, output = run_go_tests(
                root,
                pattern=lane["goTestPattern"],
                timeout=args.timeout,
                verbose=args.verbose,
            )
            transcript_path = write_transcript(output, lane_output_dir)
            if args.verbose or returncode != 0:
                print(output)
            rubrics = extract_rubrics(output)
            if returncode != 0:
                overall_go_returncode = returncode
                raw_failures.append(output)
            if selected_lane_was_skipped(output, lane["goTestPattern"]):
                msg = f"lane {lane['id']} was skipped; refusing false-green"
                print(f"[error] {msg}", file=sys.stderr)
                lane_validation_failures.append(msg)
                raw_failures.append(output)
                continue
            if len(rubrics) == 0:
                msg = f"lane {lane['id']} emitted zero rubrics; refusing false-green"
                print(f"[error] {msg}", file=sys.stderr)
                lane_validation_failures.append(msg)
                raw_failures.append(output)
                continue
            if len(rubrics) > 1:
                msg = f"lane {lane['id']} emitted {len(rubrics)} rubrics; expected exactly 1"
                print(f"[error] {msg}", file=sys.stderr)
                lane_validation_failures.append(msg)
                raw_failures.append(output)
                continue
            criteria = rubrics[0].get("criteria") or []
            if len(criteria) == 0:
                msg = f"lane {lane['id']} emitted zero criteria; refusing false-green"
                print(f"[error] {msg}", file=sys.stderr)
                lane_validation_failures.append(msg)
                raw_failures.append(output)
                continue
            for rub in rubrics:
                path = write_lane_rubric(rub, lane, lane_output_dir, transcript_path)
                all_rubrics.append(json.loads(path.read_text()))
                print(f"[run-agent-campaign] wrote rubric → {path}")
        if lane_validation_failures and overall_go_returncode == 0:
            overall_go_returncode = 1
    else:
        print(f"[run-agent-campaign] pattern={args.campaign!r} timeout={args.timeout}s")
        returncode, output = run_go_tests(
            root,
            pattern=args.campaign,
            timeout=args.timeout,
            verbose=args.verbose,
        )

        if args.verbose or returncode != 0:
            print(output)

        rubrics = extract_rubrics(output)
        overall_go_returncode = returncode
        for rub in rubrics:
            path = write_rubric(rub, output_dir)
            all_rubrics.append(rub)
            print(f"[run-agent-campaign] wrote rubric → {path}")
        if not rubrics and returncode != 0:
            raw_failures.append(output)

    exit_code = print_summary(all_rubrics, overall_go_returncode)

    if raw_failures:
        print("\n[run-agent-campaign] raw go test output (last 40 lines):")
        for output in raw_failures:
            for line in output.splitlines()[-40:]:
                print("  " + line)

    sys.exit(exit_code)


if __name__ == "__main__":
    main()
