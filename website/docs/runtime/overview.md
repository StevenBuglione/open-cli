---
title: Runtime Overview
---

# Runtime Overview

**Read this if** you are deploying `open-cli-toolbox`, debugging policy or auth behavior, or evaluating the runtime as a shared enforcement point. This page answers: what does the runtime own, what is its HTTP surface, and how does the request lifecycle work end to end.

`open-cli-toolbox` is the execution and policy plane for `open-cli`.

## Responsibilities

The runtime owns these concerns:

- loading and validating effective configuration
- discovering API descriptions from configured sources
- applying overlays and loading skill/workflow documents
- building the normalized tool catalog and effective views
- selecting the active view for a mode/profile combination
- resolving auth and secret references
- enforcing policy and approval checks
- executing upstream HTTP requests with basic retry logic
- writing audit events
- exposing cache/refresh results over HTTP

## HTTP surface

The current runtime API is intentionally small:

- `GET /v1/catalog/effective`
- `POST /v1/tools/execute`
- `POST /v1/workflows/run`
- `GET` or `POST /v1/refresh`
- `GET /v1/audit/events`
- `GET /v1/runtime/info`
- `GET /v1/auth/browser-config`

See [HTTP API](./http-api) for request and response examples.

## Remote-only deployment model

`open-cli` always talks to `open-cli-toolbox` over HTTP.

- For development, you can host `open-cli-toolbox` on loopback (`127.0.0.1:8765`).
- For localhost evaluation, prefer the repo's Docker Compose Authentik bootstrap so loopback still enforces bearer auth.
- For shared deployments, host it on infrastructure you control and point `open-cli` at the runtime URL.
- `runtime.mode` must be `remote`; embedded and local-daemon modes are no longer supported.

## Default config path behavior

If you start the runtime with `--config /path/to/.cli.json`, that path becomes the runtime's default config for HTTP requests. External callers may then omit the `config` query/body field.

If you do not set a default config, runtime requests must provide one.

## Request lifecycle

A normal `open-cli` request looks like this:

1. resolve runtime location from `--runtime`, `OPEN_CLI_RUNTIME_URL`, or `runtime.remote.url`
2. fetch the effective catalog
3. build the Cobra command tree
4. execute a selected tool or built-in command
5. let the runtime re-evaluate policy and auth
6. return output to the caller

Because catalog resolution happens so early, runtime availability affects even CLI help and schema inspection.

## Runtime auth for hosted deployments

For remote runtimes, `open-cli` can attach runtime-level bearer auth before those HTTP calls. The current client supports:

- forwarding an operator-provided token (`runtime.remote.oauth.mode: "providedToken"`)
- acquiring a client-credentials token (`runtime.remote.oauth.mode: "oauthClient"`)
- browser login against runtime-hosted metadata (`runtime.remote.oauth.mode: "browserLogin"`)

When `runtime.server.auth` is enabled on `open-cli-toolbox`, the runtime becomes the authorization boundary for remote callers:

- bearer auth is required on runtime HTTP requests
- the effective catalog is filtered by runtime scopes
- execution is denied outside the resolved authorization envelope
- `GET /v1/runtime/info` advertises whether auth is required, which validation profile is active, the scope prefixes used to derive the authorization envelope, and the envelope metadata version for compatibility checks
- `GET /v1/auth/browser-config` exposes the browser-login metadata plus the same scope and envelope diagnostics that remote clients need before starting an interactive sign-in flow
- when a request is already authenticated, the runtime handshake echoes the resolved principal for diagnostics and operator troubleshooting

Local secured runtime example:

- `examples/local-authentik/README.md` in the repo for the Docker Compose Authentik + toolbox flow
- [Authentik reference proof](./authentik-reference) for the full broker-backed OAuth proof

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Choose how to host the supported remote runtime | [Deployment models](./deployment-models) |
| Enable runtime bearer auth and catalog filtering | [Security overview](../security/overview) |
| Review HTTP API request/response shapes | [HTTP API](./http-api) |
| Evaluate enterprise or brokered deployment readiness | [Enterprise readiness](./enterprise-readiness) |
| See the broker-neutral worked example with Authentik | [Authentik reference proof](./authentik-reference) |
| Understand how cache and refresh work at runtime | [Refresh and audit](./refresh-and-audit) |

If you are evaluating whether the runtime is ready for a hosted or brokered deployment, use [Enterprise readiness](./enterprise-readiness) as the curated next step.
