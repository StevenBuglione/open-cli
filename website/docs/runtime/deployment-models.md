---
title: Deployment Models
---

# Deployment Models

The current repo supports three practical ways to run the system, and `.cli.json` can now choose between them through `runtime.mode`.

## 1. Embedded mode

Best for:

- one-off CLI use
- CI jobs
- local debugging when you do not want a background daemon

Command pattern:

```bash
./bin/oascli --embedded --config ./.cli.json catalog list --format pretty
```

Characteristics:

- no separate `oasclird` process
- cache and audit paths still come from instance resolution
- behavior matches the runtime server implementation closely
- no `runtime.json` registry entry is written

## 2. One reusable local daemon

Best for:

- interactive local development
- agents that will run many commands against the same config
- sharing a warmed cache across many `oascli` invocations

Start the daemon:

```bash
./bin/oasclird --config ./.cli.json --addr 127.0.0.1:8765
```

Then call it:

```bash
./bin/oascli --runtime http://127.0.0.1:8765 --config ./.cli.json catalog list
```

You can also select local-daemon behavior from config:

```json
{
  "runtime": {
    "mode": "local"
  }
}
```

With `runtime.mode: "auto"`, `oascli` now promotes to local-daemon mode automatically when the effective config contains local MCP `stdio` sources. If no live runtime is registered for that instance, it starts a managed local `oasclird`.

## 3. Remote runtime

Best for:

- centralized runtime hosting
- agent environments that should not run local MCP processes
- deployments where runtime access is controlled separately from the local workstation

Example:

```json
{
  "runtime": {
    "mode": "remote",
    "remote": {
      "url": "https://runtime.example.com",
      "oauth": {
        "mode": "providedToken",
        "tokenRef": "env:OAS_REMOTE_TOKEN"
      }
    }
  }
}
```

Current remote auth support:

- `providedToken` forwards a caller-supplied bearer token
- `oauthClient` acquires a client-credentials token before runtime requests
- `browserLogin` fetches runtime-hosted browser metadata and completes an authorization-code + PKCE flow

Brokered deployments typically pair those client modes with `runtime.server.auth.validationProfile: "oidc_jwks"` so the daemon can validate broker-issued runtime tokens locally.

This repo now ships one official worked example:

- [Authentik reference proof](./authentik-reference)

That page documents the broker-neutral contract, the automated `oauthClient` proof, and the manual Entra-federated browser proof.

## 4. Multiple isolated instances

Best for:

- different configs that should not share cache, audit, or runtime registry state
- concurrent long-running daemons
- operator-managed environments

Example:

```bash
./bin/oasclird --config /srv/team-a/.cli.json --instance-id team-a
./bin/oasclird --config /srv/team-b/.cli.json --instance-id team-b
```

Each instance gets its own:

- state directory
- `runtime.json`
- audit log
- cache directory

## Runtime resolution order in `oascli`

When `--embedded` is **not** set, `oascli` resolves the runtime in this order:

1. explicit runtime deployment choice from config when present
2. `--runtime`
3. `OASCLI_RUNTIME_URL`
4. instance registry (`runtime.json`) for the selected instance
5. local managed-runtime startup when config selects local mode and no live registry exists
6. fallback to `http://127.0.0.1:8765`

For explicit lifecycle control and discovery, the daemon now also exposes `GET /v1/runtime/info` and `POST /v1/runtime/stop`.

The instance selection itself can come from:

1. `--instance-id`
2. `OASCLI_INSTANCE_ID`
3. derived ID from the config path
4. `default`

If you pass `--state-dir` (or `OASCLI_STATE_DIR`), `oascli` also derives the cache root under that state directory.

## Stale runtime registry fallback

If `runtime.json` points at a daemon whose recorded PID is no longer alive, or at a runtime URL that no longer answers TCP connections, `oascli`:

1. terminates any recorded managed MCP child processes for that dead daemon
2. removes the stale registry file
3. ignores that URL
4. either restarts the managed local runtime or falls back to the default runtime URL, depending on deployment mode

Managed `oasclird` also removes its own `runtime.json` on shutdown so normal stop/close flows do not leave behind stale registry entries.

What it does **not** do:

- it does not search for another daemon automatically
- it does not persist a new URL unless a daemon writes a fresh `runtime.json`

## Security note for remote bindings

`oasclird` has no built-in auth for its own HTTP API. If you bind beyond localhost, use an external reverse proxy, firewall, SSH tunnel, or similar network controls.
