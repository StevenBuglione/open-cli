#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> Stopping services..."
docker compose -f mcp/remote/docker-compose.yml down --remove-orphans 2>/dev/null || true
docker compose down --remove-orphans 2>/dev/null || true
echo "==> Services stopped."
