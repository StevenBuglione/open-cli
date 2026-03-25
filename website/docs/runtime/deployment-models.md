---
title: Deployment Models
---

# Deployment Models

**Read this if** you are choosing how to run `ocli` — one-off embedded, a reusable local daemon, or a remote runtime — and need to understand runtime resolution order, instance isolation, and what happens when a registry entry goes stale.

The current repo supports three practical ways to run the system, and `.cli.json` can now choose between them through `runtime.mode`.

## 1. Embedded mode

Best for:

- one-off CLI use
- CI jobs
- local debugging when you do not want a background daemon

Command pattern:

```bash
./bin/ocli --embedded --config ./.cli.json catalog list --format pretty
```

Characteristics:

- no separate `oclird` process
- cache and audit paths still come from instance resolution
- behavior matches the runtime server implementation closely
- no `runtime.json` registry entry is written

## 2. One reusable local daemon

Best for:

- interactive local development
- agents that will run many commands against the same config
- sharing a warmed cache across many `ocli` invocations

Start the daemon:

```bash
./bin/oclird --config ./.cli.json --addr 127.0.0.1:8765
```

Then call it:

```bash
./bin/ocli --runtime http://127.0.0.1:8765 --config ./.cli.json catalog list
```

You can also select local-daemon behavior from config:

```json
{
  "runtime": {
    "mode": "local"
  }
}
```

With `runtime.mode: "auto"`, `ocli` now promotes to local-daemon mode automatically when the effective config contains local MCP `stdio` sources. If no live runtime is registered for that instance, it starts a managed local `oclird`.

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

If you need delegated sub-agent access in a remote deployment, keep that flow outside `oclird` itself:

- the parent runtime token goes to a broker or gateway token-exchange layer
- that layer returns a separate child token for audience `oclird`
- the child token should be short-lived and scope-subset-only relative to the parent token
- local config, curated mode, and agent profiles may restrict the child further, but they never expand access beyond the child token scopes

This is a deployment concern, not a finished CLI feature. The current docs intentionally describe the operator-owned broker pattern, not a dedicated `ocli` delegation UX.

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
./bin/oclird --config /srv/team-a/.cli.json --instance-id team-a
./bin/oclird --config /srv/team-b/.cli.json --instance-id team-b
```

Each instance gets its own:

- state directory
- `runtime.json`
- audit log
- cache directory

## Runtime resolution order in `ocli`

When `--embedded` is **not** set, `ocli` resolves the runtime in this order:

1. explicit runtime deployment choice from config when present
2. `--runtime`
3. `OCLI_RUNTIME_URL`
4. instance registry (`runtime.json`) for the selected instance
5. local managed-runtime startup when config selects local mode and no live registry exists
6. fallback to `http://127.0.0.1:8765`

For explicit lifecycle control and discovery, the daemon now also exposes `GET /v1/runtime/info` and `POST /v1/runtime/stop`.

The instance selection itself can come from:

1. `--instance-id`
2. `OCLI_INSTANCE_ID`
3. derived ID from the config path
4. `default`

If you pass `--state-dir` (or `OCLI_STATE_DIR`), `ocli` also derives the cache root under that state directory.

## Stale runtime registry fallback

If `runtime.json` points at a daemon whose recorded PID is no longer alive, or at a runtime URL that no longer answers TCP connections, `ocli`:

1. terminates any recorded managed MCP child processes for that dead daemon
2. removes the stale registry file
3. ignores that URL
4. either restarts the managed local runtime or falls back to the default runtime URL, depending on deployment mode

Managed `oclird` also removes its own `runtime.json` on shutdown so normal stop/close flows do not leave behind stale registry entries.

What it does **not** do:

- it does not search for another daemon automatically
- it does not persist a new URL unless a daemon writes a fresh `runtime.json`

## Security note for remote bindings

`oclird` is safe-by-default for local development because it binds to loopback by default, but it does have an optional built-in runtime auth layer when you enable `runtime.server.auth`.

If you bind beyond localhost:

- enable `runtime.server.auth` so the daemon validates runtime bearer tokens and filters the catalog by runtime scopes
- keep network controls in place as a second boundary, such as a reverse proxy, firewall policy, or SSH tunnel

See [Runtime Overview](./overview) for the runtime-auth handshake surface and [Authentik reference proof](./authentik-reference) for the worked brokered example.

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Configure client auth for a remote runtime | [Runtime overview](./overview) |
| Enable runtime bearer token validation on the daemon | [Security overview](../security/overview) |
| Isolate multiple teams with separate state and audit logs | [Tracing and instances](../operations/tracing-and-instances) |
| See the full brokered deployment worked example | [Authentik reference proof](./authentik-reference) |
| Evaluate enterprise readiness as an operator | [Enterprise readiness](./enterprise-readiness) |

For the evaluator-focused path that ties those pages together with fleet proof and operations, continue to [Enterprise readiness](./enterprise-readiness).
