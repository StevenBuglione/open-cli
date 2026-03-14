---
title: Operations Overview
---

# Operations Overview

Operationally, `oas-cli-go` is mostly about **state management**:

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

If `oascli` is given `--state-dir`, it also derives the cache root under `<state-dir>/cache`.

## Core operational tasks

- inspect the effective catalog
- refresh sources and inspect cache outcomes
- review audit events
- manage multiple isolated instances
- rotate or archive audit logs
- clear cache/state intentionally when debugging discovery problems

## What the runtime persists

- `runtime.json`: daemon URL plus audit/cache hints
- `audit.log`: newline-delimited JSON audit events
- cache metadata/body pairs for remote fetches

Continue with:

- [Audit logging](./audit-logging)
- [Cache and refresh](./cache-and-refresh)
- [Tracing and instances](./tracing-and-instances)
