---
title: Tracing and Instances
---

# Tracing and Instances

**Read this if** you are running multiple isolated `open-cli-toolbox` instances, need deterministic instance IDs, or want to understand the observability hooks available in the runtime. This page answers: how instance IDs are derived, what `runtime.json` contains, and how runtime state stays isolated.

Instances keep runtime state isolated. Observability hooks make it possible to inspect what the runtime is doing internally.

## Instance ID derivation

The runtime chooses an instance ID like this:

1. explicit `--instance-id` (slugified)
2. derived from the config path
3. fallback to `default`

Derived IDs are based on:

- the parent directory name of the config path
- plus the first 10 hex chars of a SHA-256 hash of the absolute path

That makes IDs deterministic without leaking full paths.

## `runtime.json`

A running hosted runtime writes:

```json
{
  "instanceId": "team-a",
  "url": "http://127.0.0.1:9031",
  "pid": 42421,
  "auditPath": "/state/instances/team-a/audit.log",
  "cacheDir": "/cache/instances/team-a/http"
}
```

Treat this as operator metadata. In the remote-only model, `ocli` does not auto-discover or auto-promote runtimes from `runtime.json`.

## Shutdown behavior

`open-cli-toolbox` removes `runtime.json` on normal shutdown.

If the process dies unexpectedly, the file can become stale. Clean it up as part of operator troubleshooting rather than expecting `ocli` to attach to it automatically.

## Observability hooks

The runtime and cache layers emit observer events and spans through `pkg/obs.Observer`.

Built-in implementations in the repo are:

- `obs.NewNop()`
- `obs.NewRecorder()` for tests

There is no built-in OpenTelemetry exporter or on-disk trace sink. If you need production trace export, `obs.Observer` is the extension point — you implement the interface and wire it in. There is no ready-made OTEL integration today; plan this as an operator-owned integration if your environment requires distributed tracing.

## Request IDs

The runtime uses `X-Request-ID` if the caller provides one. Otherwise it generates a timestamp-based request ID and attaches it to observer context.

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Start multiple isolated hosted runtime instances | See [Practical multi-instance pattern](#practical-multi-instance-pattern) in this page |
| Understand state and audit paths for an instance | [Operations overview](./overview) |
| Plug in a custom observer for testing | See `pkg/obs.Observer` — `obs.NewRecorder()` is the test implementation |
| Add OpenTelemetry export | Not available today; `obs.Observer` is the extension point |

## Practical multi-instance pattern

```bash
./bin/open-cli-toolbox --config /srv/team-a/.cli.json --instance-id team-a --state-dir /var/lib/ocli
./bin/open-cli-toolbox --config /srv/team-b/.cli.json --instance-id team-b --state-dir /var/lib/ocli
```

Then callers select the matching runtime with:

```bash
./bin/ocli --runtime http://127.0.0.1:9031 --config /srv/team-a/.cli.json --instance-id team-a --state-dir /var/lib/ocli catalog list
```
