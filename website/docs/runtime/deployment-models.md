---
title: Deployment Models
---

# Deployment Models

**Read this if** you are choosing how to host `open-cli-toolbox` and need to understand the supported remote-only model, runtime resolution order, instance isolation, and the operator-owned security boundary.

The current implementation supports **one runtime model**: `ocli` connects to a hosted `open-cli-toolbox` server over HTTP. What changes is where you host that runtime and how you secure it.

## 1. Loopback-hosted runtime

Best for:

- local evaluation
- contributor workflows
- single-user debugging when you want the production contract without shared infra

Command pattern:

```bash
open-cli-toolbox --config ./.cli.json --addr 127.0.0.1:8765

# In another shell:
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json catalog list --format pretty
```

Characteristics:

- same HTTP contract as every other supported deployment
- easy to inspect logs, cache, and audit files locally
- no embedded mode or local-daemon mode hidden behind the CLI

## 2. Shared remote runtime

Best for:

- centralized runtime hosting
- agents and automation that should not start local processes
- teams that need one governed catalog, shared cache, and common policy boundary

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

## 3. Brokered enterprise runtime

Best for:

- enterprise access review
- centrally issued runtime tokens
- deployments where runtime access is controlled separately from workstations and agents

Brokered deployments typically pair those client modes with `runtime.server.auth.validationProfile: "oidc_jwks"` so `open-cli-toolbox` can validate broker-issued runtime tokens locally.

If you need delegated sub-agent access in a remote deployment, keep that flow outside `open-cli-toolbox` itself:

- the parent runtime token goes to a broker or gateway token-exchange layer
- that layer returns a separate child token for audience `open-cli-toolbox`
- the child token should be short-lived and scope-subset-only relative to the parent token
- local config, curated mode, and agent profiles may restrict the child further, but they never expand access beyond the child token scopes

This is a deployment concern, not a finished CLI feature. The current docs intentionally describe the operator-owned broker pattern, not a dedicated `ocli` delegation UX.

This repo now ships one official worked example:

- [Authentik reference proof](./authentik-reference)

That page documents the broker-neutral contract, the automated `oauthClient` proof, and the manual Entra-federated browser proof.

## 4. Multiple isolated instances

Best for:

- different configs that should not share cache, audit, or runtime metadata state
- multiple hosted runtimes on one machine
- operator-managed environments with strict separation requirements

Example:

```bash
./bin/open-cli-toolbox --config /srv/team-a/.cli.json --instance-id team-a --state-dir /var/lib/ocli
./bin/open-cli-toolbox --config /srv/team-b/.cli.json --instance-id team-b --state-dir /var/lib/ocli
```

Each instance gets its own:

- state directory
- `runtime.json` metadata file
- audit log
- cache directory

## Runtime resolution order in `ocli`

`ocli` resolves the runtime in this order:

1. explicit `--runtime`
2. `OCLI_RUNTIME_URL`
3. `runtime.remote.url` from the effective config

If none of those are available, the command fails. `ocli` no longer auto-starts or auto-discovers a local runtime.

## `runtime.json` in the remote-only model

`open-cli-toolbox` writes `runtime.json` under the selected state directory so operators can inspect instance metadata such as URL, PID, audit path, and cache directory.

In the remote-only model, `ocli` does **not** promote or attach to runtimes automatically from that file. Treat it as operator-facing metadata, not as a supported client-discovery mechanism.

## Security note for remote bindings

`open-cli-toolbox` defaults to loopback when you pass no `--addr`, which is convenient for local evaluation. For shared or production use:

- enable `runtime.server.auth` so the runtime validates bearer tokens and filters the catalog by runtime scopes
- keep network controls in place as a second boundary, such as a reverse proxy, firewall policy, or SSH tunnel
- document who owns the runtime URL, config path, and state directory in each environment

See [Runtime Overview](./overview) for the runtime-auth handshake surface and [Authentik reference proof](./authentik-reference) for the worked brokered example.

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Configure client auth for a remote runtime | [Runtime overview](./overview) |
| Enable runtime bearer token validation on the hosted runtime | [Security overview](../security/overview) |
| Isolate multiple teams with separate state and audit logs | [Tracing and instances](../operations/tracing-and-instances) |
| See the full brokered deployment worked example | [Authentik reference proof](./authentik-reference) |
| Evaluate enterprise readiness as an operator | [Enterprise readiness](./enterprise-readiness) |

For the evaluator-focused path that ties those pages together with fleet proof and operations, continue to [Enterprise readiness](./enterprise-readiness).
