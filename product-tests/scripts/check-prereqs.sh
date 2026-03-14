#!/usr/bin/env bash
set -euo pipefail

fail() { echo "MISSING: $1 — please install it before running product tests" >&2; exit 1; }

echo "==> Checking prerequisites..."

command -v go     >/dev/null 2>&1 || fail "go"
command -v python3 >/dev/null 2>&1 || fail "python3"
command -v docker >/dev/null 2>&1 || fail "docker"
command -v npx    >/dev/null 2>&1 || fail "npx"

# Verify docker compose plugin (not standalone docker-compose)
docker compose version >/dev/null 2>&1 || fail "docker compose plugin"

echo "    go:      $(go version | awk '{print $3}')"
echo "    python3: $(python3 --version)"
echo "    docker:  $(docker version --format '{{.Client.Version}}' 2>/dev/null)"
echo "    npx:     $(npx --version)"
echo "==> All prerequisites satisfied."
