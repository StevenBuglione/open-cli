#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$REPO_ROOT/product-tests/authentik/docker-compose.yml"
BOOTSTRAP_SCRIPT="$SCRIPT_DIR/bootstrap-authentik.py"

RUNTIME_CONFIG_PATH="${OCLI_RUNTIME_CONFIG_PATH:-${OCLI_DAEMON_CONFIG_PATH:-$REPO_ROOT/config/.cli.authentik.local.json}}"
CONFIG_PATH="${OCLI_LOCAL_CONFIG_PATH:-$REPO_ROOT/.cli.local.json}"
BROWSER_CONFIG_PATH="${OCLI_BROWSER_CONFIG_PATH:-$REPO_ROOT/.browser-login/.cli.json}"
ENV_PATH="${OCLI_LOCAL_ENV_PATH:-$REPO_ROOT/.authentik.local.env}"
DOCKER_ENV_PATH="${OCLI_DOCKER_ENV_PATH:-$REPO_ROOT/.authentik.docker.env}"
AUTHENTIK_BASE_URL="${AUTHENTIK_BASE_URL:-http://127.0.0.1:9100}"
DEFAULT_RUNTIME_AUTHENTIK_BASE_URL="$(python3 - <<'PY' "$AUTHENTIK_BASE_URL"
from urllib.parse import urlparse, urlunparse
import sys

parsed = urlparse(sys.argv[1])
hostname = parsed.hostname or ""
if hostname in {"127.0.0.1", "localhost"}:
    netloc = parsed.netloc.replace(hostname, "host.docker.internal", 1)
    parsed = parsed._replace(netloc=netloc)
print(urlunparse(parsed))
PY
)"
RUNTIME_AUTHENTIK_BASE_URL="${OCLI_RUNTIME_AUTHENTIK_BASE_URL:-${OCLI_DAEMON_AUTHENTIK_BASE_URL:-$DEFAULT_RUNTIME_AUTHENTIK_BASE_URL}}"
RUNTIME_URL="${OCLI_RUNTIME_URL:-http://127.0.0.1:8765}"
RUNTIME_AUDIENCE="${OCLI_RUNTIME_AUDIENCE:-open-cli-toolbox}"
RUNTIME_SERVICE_ID="${OCLI_RUNTIME_SERVICE_ID:-testapi}"
RUNTIME_EXTRA_SERVICE_IDS="${OCLI_RUNTIME_EXTRA_SERVICE_IDS:-}"
RUNTIME_EXTRA_SCOPES="${OCLI_RUNTIME_EXTRA_SCOPES:-}"
OPENAPI_URI="${OCLI_OPENAPI_URI:-./product-tests/testdata/openapi/testapi.openapi.yaml}"
AUTHENTIK_CLIENT_SLUG="${OCLI_AUTHENTIK_CLIENT_SLUG:-ocli-runtime-local}"
AUTHENTIK_PROVIDER_NAME="${OCLI_AUTHENTIK_PROVIDER_NAME:-ocli Runtime Local Provider}"
AUTHENTIK_APPLICATION_NAME="${OCLI_AUTHENTIK_APPLICATION_NAME:-ocli Runtime Local}"
AUTHENTIK_REDIRECT_URI="${OCLI_AUTHENTIK_REDIRECT_URI:-http://127.0.0.1:8787/callback}"
BROWSER_CALLBACK_PORT="${OCLI_BROWSER_CALLBACK_PORT:-8787}"
AUTHENTIK_BROWSER_CLIENT_SLUG="${OCLI_AUTHENTIK_BROWSER_CLIENT_SLUG:-ocli-runtime-browser-local}"
AUTHENTIK_BROWSER_PROVIDER_NAME="${OCLI_AUTHENTIK_BROWSER_PROVIDER_NAME:-ocli Runtime Browser Local Provider}"
AUTHENTIK_BROWSER_APPLICATION_NAME="${OCLI_AUTHENTIK_BROWSER_APPLICATION_NAME:-ocli Runtime Browser Local}"
AUTHENTIK_BROWSER_REDIRECT_URI="${OCLI_AUTHENTIK_BROWSER_REDIRECT_URI:-http://127.0.0.1:${BROWSER_CALLBACK_PORT}/callback}"
AUTHENTIK_ACCESS_TOKEN_VALIDITY="${OCLI_AUTHENTIK_ACCESS_TOKEN_VALIDITY:-hours=1}"
RUNTIME_BUNDLE_SCOPE="bundle:${RUNTIME_SERVICE_ID}"
BOOTSTRAP_EXTRA_SCOPES="$(python3 - <<'PY' "$RUNTIME_EXTRA_SERVICE_IDS" "$RUNTIME_EXTRA_SCOPES"
import sys

extra_service_ids = [item for item in sys.argv[1].replace(",", " ").split() if item]
extra_scopes = [item for item in sys.argv[2].split() if item]
scopes = [*(f"bundle:{service_id}" for service_id in extra_service_ids), *extra_scopes]
print(" ".join(scopes))
PY
)"
DOCKER_WITH_SG=0

docker_cmd() {
  if [[ "$DOCKER_WITH_SG" -eq 1 ]]; then
    local command
    printf -v command '%q ' docker "$@"
    sg docker -c "$command"
    return
  fi
  docker "$@"
}

if ! docker info >/dev/null 2>&1; then
  if sg docker -c 'docker info >/dev/null 2>&1'; then
    DOCKER_WITH_SG=1
  else
    echo "docker is installed but this shell cannot access the Docker daemon" >&2
    exit 1
  fi
fi

wait_for_worker() {
  local deadline=$((SECONDS + 120))
  local worker_id=""
  while [[ $SECONDS -lt $deadline ]]; do
    worker_id="$(docker_cmd compose -f "$COMPOSE_FILE" ps -q worker)"
    if [[ -n "$worker_id" ]]; then
      printf '%s\n' "$worker_id"
      return 0
    fi
    sleep 2
  done
  return 1
}

extract_bootstrap_json() {
  python3 -c 'import json, sys
text = sys.stdin.read()
marker = "__OCLI_JSON__="
index = text.rfind(marker)
if index == -1:
    raise SystemExit(1)
payload = json.loads(text[index + len(marker):].strip())
print(json.dumps(payload))'
}

json_field() {
  local field="$1"
  python3 -c 'import json, sys
payload = json.load(sys.stdin)
print(payload[sys.argv[1]])' "$field"
}

fetch_discovery() {
  local discovery_url="$1"
  local deadline=$((SECONDS + 120))
  while [[ $SECONDS -lt $deadline ]]; do
    if curl --fail --silent --show-error "$discovery_url"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

bootstrap_provider() {
  local worker_id="$1"
  local client_type="$2"
  local client_slug="$3"
  local provider_name="$4"
  local application_name="$5"
  local redirect_uri="$6"
  local encoded_script
  encoded_script="$(base64 -w0 "$BOOTSTRAP_SCRIPT")"
  docker_cmd exec \
    -e OCLI_BOOTSTRAP_SCRIPT_B64="$encoded_script" \
    -e OCLI_AUTHENTIK_CLIENT_TYPE="$client_type" \
    -e OCLI_RUNTIME_AUDIENCE="$RUNTIME_AUDIENCE" \
    -e OCLI_RUNTIME_SERVICE_ID="$RUNTIME_SERVICE_ID" \
    -e OCLI_RUNTIME_EXTRA_SCOPES="$BOOTSTRAP_EXTRA_SCOPES" \
    -e OCLI_AUTHENTIK_CLIENT_SLUG="$client_slug" \
    -e OCLI_AUTHENTIK_PROVIDER_NAME="$provider_name" \
    -e OCLI_AUTHENTIK_APPLICATION_NAME="$application_name" \
    -e OCLI_AUTHENTIK_REDIRECT_URI="$redirect_uri" \
    -e OCLI_AUTHENTIK_ACCESS_TOKEN_VALIDITY="$AUTHENTIK_ACCESS_TOKEN_VALIDITY" \
    "$worker_id" \
    /ak-root/venv/bin/python /manage.py shell -c "import base64, os; exec(base64.b64decode(os.environ['OCLI_BOOTSTRAP_SCRIPT_B64']).decode())"
}

bootstrap_provider_json() {
  local worker_id="$1"
  local client_type="$2"
  local client_slug="$3"
  local provider_name="$4"
  local application_name="$5"
  local redirect_uri="$6"
  local bootstrap_output=""
  local bootstrap_json=""
  local bootstrap_deadline=$((SECONDS + 120))
  while [[ $SECONDS -lt $bootstrap_deadline ]]; do
    if bootstrap_output="$(bootstrap_provider "$worker_id" "$client_type" "$client_slug" "$provider_name" "$application_name" "$redirect_uri" 2>&1)"; then
      if bootstrap_json="$(printf '%s' "$bootstrap_output" | extract_bootstrap_json 2>/dev/null)"; then
        printf '%s\n' "$bootstrap_json"
        return 0
      fi
    fi
    sleep 2
  done
  echo "failed to bootstrap Authentik provider" >&2
  printf '%s\n' "$bootstrap_output" >&2
  return 1
}

echo "==> Starting Authentik reference stack..."
docker_cmd compose -f "$COMPOSE_FILE" up -d >/dev/null

echo "==> Waiting for Authentik worker..."
worker_id="$(wait_for_worker)" || {
  echo "failed to find Authentik worker container" >&2
  exit 1
}

echo "==> Bootstrapping local Authentik runtime providers..."
bootstrap_json="$(bootstrap_provider_json "$worker_id" "confidential" "$AUTHENTIK_CLIENT_SLUG" "$AUTHENTIK_PROVIDER_NAME" "$AUTHENTIK_APPLICATION_NAME" "$AUTHENTIK_REDIRECT_URI")"
browser_bootstrap_json="$(bootstrap_provider_json "$worker_id" "public" "$AUTHENTIK_BROWSER_CLIENT_SLUG" "$AUTHENTIK_BROWSER_PROVIDER_NAME" "$AUTHENTIK_BROWSER_APPLICATION_NAME" "$AUTHENTIK_BROWSER_REDIRECT_URI")"

client_slug="$(printf '%s' "$bootstrap_json" | json_field slug)"
client_id="$(printf '%s' "$bootstrap_json" | json_field client_id)"
client_secret="$(printf '%s' "$bootstrap_json" | json_field client_secret)"
browser_client_slug="$(printf '%s' "$browser_bootstrap_json" | json_field slug)"
browser_client_id="$(printf '%s' "$browser_bootstrap_json" | json_field client_id)"

discovery_url="${AUTHENTIK_BASE_URL%/}/application/o/${client_slug}/.well-known/openid-configuration"
echo "==> Waiting for discovery: $discovery_url"
discovery_json="$(fetch_discovery "$discovery_url")" || {
  echo "failed to fetch Authentik discovery document" >&2
  exit 1
}
browse_discovery_url="${AUTHENTIK_BASE_URL%/}/application/o/${browser_client_slug}/.well-known/openid-configuration"
echo "==> Waiting for browser discovery: $browse_discovery_url"
browser_discovery_json="$(fetch_discovery "$browse_discovery_url")" || {
  echo "failed to fetch Authentik browser discovery document" >&2
  exit 1
}

issuer="$(printf '%s' "$discovery_json" | json_field issuer)"
jwks_url="$(printf '%s' "$discovery_json" | json_field jwks_uri)"
token_url="$(printf '%s' "$discovery_json" | json_field token_endpoint)"
browser_issuer="$(printf '%s' "$browser_discovery_json" | json_field issuer)"
browser_jwks_url="$(printf '%s' "$browser_discovery_json" | json_field jwks_uri)"
browser_token_url="$(printf '%s' "$browser_discovery_json" | json_field token_endpoint)"
browser_authorization_url="$(printf '%s' "$browser_discovery_json" | json_field authorization_endpoint)"

mkdir -p "$(dirname "$RUNTIME_CONFIG_PATH")" "$(dirname "$CONFIG_PATH")" "$(dirname "$BROWSER_CONFIG_PATH")" "$(dirname "$ENV_PATH")" "$(dirname "$DOCKER_ENV_PATH")"

python3 - <<'PY' "$ENV_PATH" "$client_id" "$client_secret"
import pathlib
import shlex
import sys

path = pathlib.Path(sys.argv[1])
client_id = sys.argv[2]
client_secret = sys.argv[3]
path.write_text(
    "export OAS_REMOTE_CLIENT_ID={}\nexport OAS_REMOTE_CLIENT_SECRET={}\n".format(
        shlex.quote(client_id),
        shlex.quote(client_secret),
    ),
    encoding="utf-8",
)
path.chmod(0o600)
PY

python3 - <<'PY' "$RUNTIME_CONFIG_PATH" "$issuer" "$jwks_url" "$token_url" "$browser_authorization_url" "$browser_client_id" "$RUNTIME_AUDIENCE" "$RUNTIME_URL" "$RUNTIME_BUNDLE_SCOPE" "$RUNTIME_SERVICE_ID" "$OPENAPI_URI" "$REPO_ROOT" "$RUNTIME_EXTRA_SERVICE_IDS" "$BOOTSTRAP_EXTRA_SCOPES" "$RUNTIME_AUTHENTIK_BASE_URL"
import json
import os
import pathlib
import sys
from urllib.parse import urlparse, urlunparse

path = pathlib.Path(sys.argv[1])
issuer = sys.argv[2]
jwks_url = sys.argv[3]
token_url = sys.argv[4]
authorization_url = sys.argv[5]
browser_client_id = sys.argv[6]
runtime_audience = sys.argv[7]
runtime_url = sys.argv[8]
bundle_scope = sys.argv[9]
service_id = sys.argv[10]
openapi_uri = sys.argv[11]
repo_root = pathlib.Path(sys.argv[12]).resolve()
extra_service_ids = [item for item in sys.argv[13].replace(",", " ").split() if item]
extra_scopes = [item for item in sys.argv[14].split() if item]
runtime_authentik_base_url = sys.argv[15]

def remap_authentik_url(url: str) -> str:
    parsed = urlparse(url)
    base = urlparse(runtime_authentik_base_url)
    return urlunparse(parsed._replace(scheme=base.scheme, netloc=base.netloc))

def rendered_uri(config_path: pathlib.Path, uri: str) -> str:
    if "://" in uri:
        return uri
    source_path = pathlib.Path(uri)
    if not source_path.is_absolute():
        source_path = repo_root / source_path
    source_path = source_path.resolve()
    return f"/workspace/{source_path.relative_to(repo_root).as_posix()}"

def unique(items):
    seen = set()
    ordered = []
    for item in items:
        if item in seen:
            continue
        seen.add(item)
        ordered.append(item)
    return ordered

service_ids = unique([service_id, *extra_service_ids])
scopes = unique([bundle_scope, *extra_scopes])
sources = {}
services = {}
for current_service_id in service_ids:
    current_source_id = f"{current_service_id}Source"
    sources[current_source_id] = {
        "type": "openapi",
        "uri": rendered_uri(path, openapi_uri),
        "enabled": True,
    }
    services[current_service_id] = {
        "source": current_source_id,
        "alias": current_service_id,
    }

config = {
    "cli": "1.0.0",
    "mode": {"default": "discover"},
    "runtime": {
        "mode": "remote",
        "server": {
            "auth": {
                "validationProfile": "oidc_jwks",
                "issuer": issuer,
                "jwksURL": remap_authentik_url(jwks_url),
                "audience": runtime_audience,
                "authorizationURL": authorization_url,
                "tokenURL": token_url,
                "browserClientId": browser_client_id,
            }
        },
        "remote": {
            "url": runtime_url,
            "oauth": {
                "mode": "oauthClient",
                "audience": runtime_audience,
                "scopes": scopes,
                "client": {
                    "tokenURL": token_url,
                    "clientId": {"type": "env", "value": "OAS_REMOTE_CLIENT_ID"},
                    "clientSecret": {"type": "env", "value": "OAS_REMOTE_CLIENT_SECRET"},
                },
            },
        },
    },
    "sources": sources,
    "services": services,
}
path.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
PY

python3 - <<'PY' "$CONFIG_PATH" "$token_url" "$RUNTIME_AUDIENCE" "$RUNTIME_URL" "$RUNTIME_BUNDLE_SCOPE" "$BOOTSTRAP_EXTRA_SCOPES"
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
token_url = sys.argv[2]
runtime_audience = sys.argv[3]
runtime_url = sys.argv[4]
bundle_scope = sys.argv[5]
extra_scopes = [item for item in sys.argv[6].split() if item]

def unique(items):
    seen = set()
    ordered = []
    for item in items:
        if item in seen:
            continue
        seen.add(item)
        ordered.append(item)
    return ordered

config = {
    "cli": "1.0.0",
    "mode": {"default": "discover"},
    "runtime": {
        "mode": "remote",
        "remote": {
            "url": runtime_url,
            "oauth": {
                "mode": "oauthClient",
                "audience": runtime_audience,
                "scopes": unique([bundle_scope, *extra_scopes]),
                "client": {
                    "tokenURL": token_url,
                    "clientId": {"type": "env", "value": "OAS_REMOTE_CLIENT_ID"},
                    "clientSecret": {"type": "env", "value": "OAS_REMOTE_CLIENT_SECRET"},
                },
            },
        },
    },
}
path.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
PY

python3 - <<'PY' "$BROWSER_CONFIG_PATH" "$RUNTIME_AUDIENCE" "$RUNTIME_URL" "$RUNTIME_BUNDLE_SCOPE" "$BROWSER_CALLBACK_PORT" "$BOOTSTRAP_EXTRA_SCOPES"
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
runtime_audience = sys.argv[2]
runtime_url = sys.argv[3]
bundle_scope = sys.argv[4]
callback_port = int(sys.argv[5])
extra_scopes = [item for item in sys.argv[6].split() if item]

def unique(items):
    seen = set()
    ordered = []
    for item in items:
        if item in seen:
            continue
        seen.add(item)
        ordered.append(item)
    return ordered

config = {
    "cli": "1.0.0",
    "mode": {"default": "discover"},
    "runtime": {
        "mode": "remote",
        "remote": {
            "url": runtime_url,
            "oauth": {
                "mode": "browserLogin",
                "audience": runtime_audience,
                "scopes": unique([bundle_scope, *extra_scopes]),
                "browserLogin": {
                    "callbackPort": callback_port,
                },
            },
        },
    },
}
path.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
PY

python3 - <<'PY' "$DOCKER_ENV_PATH" "$RUNTIME_CONFIG_PATH" "$REPO_ROOT/config"
import pathlib
import shlex
import sys

path = pathlib.Path(sys.argv[1])
runtime_config_path = pathlib.Path(sys.argv[2]).resolve()
mounted_config_dir = pathlib.Path(sys.argv[3]).resolve()
try:
    relative_config_path = runtime_config_path.relative_to(mounted_config_dir)
except ValueError as exc:
    raise SystemExit(f"runtime config path must live under {mounted_config_dir}: {runtime_config_path}") from exc
path.write_text(
    "export OPEN_CLI_TOOLBOX_CONFIG_PATH={}\n".format(shlex.quote(f"/config/{relative_config_path.as_posix()}")),
    encoding="utf-8",
)
path.chmod(0o600)
PY

echo "==> Wrote hosted runtime config: $RUNTIME_CONFIG_PATH"
echo "==> Wrote workload client config: $CONFIG_PATH"
echo "==> Wrote browser client config: $BROWSER_CONFIG_PATH"
echo "==> Wrote client credential exports: $ENV_PATH"
echo "==> Wrote Docker runtime exports: $DOCKER_ENV_PATH"
echo
echo "Next steps:"
echo "  source \"$ENV_PATH\""
echo "  go run ./cmd/open-cli-toolbox --addr 127.0.0.1:8765 --config \"$RUNTIME_CONFIG_PATH\""
echo "  source \"$DOCKER_ENV_PATH\" && docker compose up -d open-cli-toolbox"
echo "  source \"$ENV_PATH\" && go run ./cmd/ocli --config \"$CONFIG_PATH\" catalog list --format pretty"
echo "  # browser client config written to: \"$BROWSER_CONFIG_PATH\""
echo "  # hosted browser login still needs a browser-matched runtime auth config"
echo
echo "Optional execution fixture:"
echo "  cd product-tests && make services-up"
