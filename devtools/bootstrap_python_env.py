#!/usr/bin/env python3
"""Create or reuse a local venv, install requirements, and run a command."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path
from typing import Callable, Sequence

Runner = Callable[..., subprocess.CompletedProcess[bytes] | subprocess.CompletedProcess[str]]

STAMP_PREFIX = ".bootstrap-python-env"


def venv_python(venv_dir: Path) -> Path:
    return venv_dir / "bin" / "python3"


def stamp_path(*, venv_dir: Path, requirements_path: Path) -> Path:
    requirements_key = hashlib.sha256(str(requirements_path).encode("utf-8")).hexdigest()[:12]
    return venv_dir / f"{STAMP_PREFIX}-{requirements_key}.json"


def build_stamp(*, requirements_path: Path) -> str:
    payload = {
        "requirements_path": str(requirements_path),
        "requirements_sha256": hashlib.sha256(requirements_path.read_bytes()).hexdigest(),
    }
    return json.dumps(payload, sort_keys=True)


def install_requirements(*, venv_dir: Path, requirements_path: Path, runner: Runner = subprocess.run) -> None:
    runner(
        [str(venv_python(venv_dir)), "-m", "pip", "install", "-q", "-r", str(requirements_path)],
        check=True,
    )


def ensure_venv(*, venv_dir: Path, runner: Runner = subprocess.run) -> None:
    python_path = venv_python(venv_dir)
    if python_path.exists():
        return
    runner([sys.executable, "-m", "venv", str(venv_dir)], check=True)


def ensure_pip(*, venv_dir: Path, runner: Runner = subprocess.run) -> None:
    python_path = venv_python(venv_dir)
    probe = runner(
        [str(python_path), "-m", "pip", "--version"],
        check=False,
        capture_output=True,
        text=True,
    )
    if probe.returncode == 0:
        return
    runner([str(python_path), "-m", "ensurepip", "--upgrade"], check=True)


def ensure_requirements(*, venv_dir: Path, requirements_path: Path, runner: Runner = subprocess.run) -> None:
    expected_stamp = build_stamp(requirements_path=requirements_path)
    current_stamp_path = stamp_path(venv_dir=venv_dir, requirements_path=requirements_path)
    if current_stamp_path.exists() and current_stamp_path.read_text() == expected_stamp:
        return
    install_requirements(venv_dir=venv_dir, requirements_path=requirements_path, runner=runner)
    current_stamp_path.write_text(expected_stamp)


def parse_args(argv: Sequence[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--venv", default=".venv", help="venv directory to create or reuse")
    parser.add_argument("--requirements", required=True, help="requirements.txt to install")
    parser.add_argument("command", nargs=argparse.REMAINDER, help="command to run after '--'")
    args = parser.parse_args(argv)
    if args.command and args.command[0] == "--":
        args.command = args.command[1:]
    if not args.command:
        parser.error("missing command after '--'")
    return args


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv or sys.argv[1:])
    venv_dir = Path(args.venv)
    requirements_path = Path(args.requirements)

    ensure_venv(venv_dir=venv_dir)
    ensure_pip(venv_dir=venv_dir)
    ensure_requirements(venv_dir=venv_dir, requirements_path=requirements_path)
    completed = subprocess.run(args.command, check=False)
    return completed.returncode


if __name__ == "__main__":
    raise SystemExit(main())
