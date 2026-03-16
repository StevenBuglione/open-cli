#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> Starting Authentik reference stack..."
docker compose -f authentik/docker-compose.yml up -d
echo "==> Authentik reference stack started."
