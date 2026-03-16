---
title: Runtime Overview
---

# Runtime Overview

`oasclird` is the execution and policy plane for `oascli`.

Even when you use embedded mode, the same runtime server implementation is doing the work behind the scenes.

The CLI can now pick that runtime path from `.cli.json` through `runtime.mode`, not just from CLI flags and environment variables.

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

See [HTTP API](./http-api) for request and response examples.

## Local-first deployment

`oasclird` defaults to `127.0.0.1:0`, which means:

- it binds only to localhost by default
- the OS picks an available port
- the chosen URL is written to the instance registry (`runtime.json`)

This default matters because the runtime has **no built-in caller authentication or authorization layer**. If you bind it to a broader interface, secure it with external network controls.

## Default config path behavior

If you start the daemon with `--config /path/to/.cli.json`, that path becomes the runtime's default config for HTTP requests. External callers may then omit the `config` query/body field.

If you do not set a default config, runtime requests must provide one.

## Embedded mode uses the same server logic

`oascli --embedded` creates `internal/runtime.Server` in-process and sends requests through an `httptest` recorder. This means:

- catalog building, policy, auth, cache, and audit behavior match the daemon path closely
- there is no separate `oasclird` process to manage
- there is also no runtime registry file to discover later

## Request lifecycle

A normal `oascli` request looks like this:

1. resolve runtime location or use embedded mode
2. fetch the effective catalog
3. build the Cobra command tree
4. execute a selected tool or built-in command
5. let the runtime re-evaluate policy and auth
6. return output to the caller

Because catalog resolution happens so early, runtime availability affects even CLI help and schema inspection.

For remote runtimes, `oascli` can also attach runtime-level bearer auth before those HTTP calls. The current client supports:

- forwarding an operator-provided token (`runtime.remote.oauth.mode: "providedToken"`)
- acquiring a client-credentials token (`runtime.remote.oauth.mode: "oauthClient"`)
- browser login against runtime-hosted metadata (`runtime.remote.oauth.mode: "browserLogin"`)

When `runtime.server.auth` is enabled on `oasclird`, the daemon itself now becomes the authorization boundary for remote callers:

- bearer auth is required on runtime HTTP requests
- the effective catalog is filtered by runtime scopes
- execution is denied outside the resolved authorization envelope
- `GET /v1/runtime/info` advertises whether auth is required, which validation profile is active, the scope prefixes used to derive the authorization envelope, and the envelope metadata version for compatibility checks
- `GET /v1/auth/browser-config` exposes the browser-login metadata plus the same scope/envelope diagnostics that remote clients need before starting an interactive sign-in flow
- when a request is already authenticated, the runtime handshake echoes the resolved principal for diagnostics and operator troubleshooting
