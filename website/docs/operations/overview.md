---
title: Operations Overview
---

# Operations Overview

**Read this if** you are managing a running `open-cli-toolbox` installation and need to understand the state layout, how to inspect cache and audit data, or how to isolate multiple instances. This page answers: where does state live, what does the runtime persist, and which pages go deeper on each operational concern.

Operationally, `open-cli` is mostly about **state management**:

- where the runtime is registered
- where cache entries live
- where audit events are written
- how instances stay isolated from each other

## State layout at a glance

Each instance gets a directory layout like:

```text
<state-root>/instances/<instance-id>/
  audit.log
  runtime.json

<cache-root>/instances/<instance-id>/http/
  <sha256>.json
  <sha256>.body
```

## Default roots

Without overrides:

- state root: `$XDG_STATE_HOME/oas-cli` or `~/.local/state/oas-cli`
- cache root: `$XDG_CACHE_HOME/oas-cli` or `~/.cache/oas-cli`

If `ocli` is given `--state-dir`, it also derives the cache root under `<state-dir>/cache`.

## Core operational tasks

- inspect the effective catalog
- refresh sources and inspect cache outcomes
- review audit events
- manage multiple isolated instances
- rotate or archive audit logs
- clear cache/state intentionally when debugging discovery problems

## What the runtime persists

- `runtime.json`: hosted runtime URL plus audit/cache hints
- `audit.log`: newline-delimited JSON audit events
- cache metadata/body pairs for remote fetches

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Understand what events are written and what is missing | [Audit logging](./audit-logging) |
| Expire, refresh, or inspect the HTTP cache | [Cache and refresh](./cache-and-refresh) |
| Run multiple isolated runtimes side by side | [Tracing and instances](./tracing-and-instances) |
| Override state or cache root paths | [Deployment models](../runtime/deployment-models) |

## Operational limits to know before a production deployment

These are not roadmap items — they are current facts that affect production planning:

- **No built-in log rotation or retention.** The audit file grows without bound unless you add external tooling (`logrotate`, a log sidecar, or a forwarder).
- **No audit push export.** Audit data is readable from disk or via `GET /v1/audit/events`. There is no push exporter or SIEM connector today.
- **No OpenTelemetry export.** The `obs.Observer` interface is the extension point; there is no built-in trace sink.
- **Network perimeter is operator-owned.** For remote deployments, firewall rules, reverse proxy auth, or container isolation must be provided outside the runtime.

Continue with:

- [Audit logging](./audit-logging)
- [Cache and refresh](./cache-and-refresh)
- [Tracing and instances](./tracing-and-instances)
