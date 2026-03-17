#!/usr/bin/env bash
set -euo pipefail

addr="${MCP_REMOTE_HOST:-127.0.0.1:3001}"
deadline=$((SECONDS + 60))

echo "==> Waiting for MCP remote endpoint at http://${addr}/mcp ..."

while (( SECONDS < deadline )); do
  if code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 2 -X POST "http://${addr}/mcp" 2>/dev/null)"; then
    echo "==> MCP remote endpoint ready (HTTP ${code})"
    exit 0
  fi
  sleep 1
done

echo "MCP remote endpoint did not become ready within 60 seconds" >&2
exit 1
