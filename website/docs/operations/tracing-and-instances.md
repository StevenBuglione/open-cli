---
title: Tracing and Instances
---

# Tracing and Instances

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

A running daemon writes:

```json
{
  "instanceId": "team-a",
  "url": "http://127.0.0.1:9031",
  "auditPath": "/state/instances/team-a/audit.log",
  "cacheDir": "/cache/instances/team-a/http"
}
```

`oascli` reads this file during runtime resolution.

## Stale registry cleanup

If the stored URL no longer accepts TCP connections, `oascli` deletes the stale `runtime.json` file and falls back to the default runtime URL.

## Observability hooks

The runtime and cache layers emit observer events and spans through `pkg/obs.Observer`.

Built-in implementations in the repo are:

- `obs.NewNop()`
- `obs.NewRecorder()` for tests

There is no built-in OpenTelemetry exporter or on-disk trace sink.

## Request IDs

The runtime uses `X-Request-ID` if the caller provides one. Otherwise it generates a timestamp-based request ID and attaches it to observer context.

## Practical multi-instance pattern

```bash
./bin/oasclird --config /srv/team-a/.cli.json --instance-id team-a --state-dir /var/lib/oascli
./bin/oasclird --config /srv/team-b/.cli.json --instance-id team-b --state-dir /var/lib/oascli
```

Then callers select the matching instance with:

```bash
./bin/oascli --config /srv/team-a/.cli.json --instance-id team-a --state-dir /var/lib/oascli catalog list
```
