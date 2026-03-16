#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> Stopping Authentik reference stack..."
docker compose -f authentik/docker-compose.yml down --remove-orphans
echo "==> Authentik reference stack stopped."
