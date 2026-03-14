#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> Starting services..."

compose_up() {
  local args=("$@")
  # Skip if the compose file defines no services (placeholder scaffolds)
  if [[ -z "$(docker compose "${args[@]}" config --services 2>/dev/null)" ]]; then
    echo "    (no services defined in compose${args[*]:+ for ${args[*]}}, skipping)"
    return 0
  fi
  docker compose "${args[@]}" up -d
}

compose_up
compose_up -f mcp/remote/docker-compose.yml

echo "==> Services started."
