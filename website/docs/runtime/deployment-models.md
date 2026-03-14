---
title: Deployment Models
---

# Deployment Models

The current repo supports three practical ways to run the system.

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

## 3. Multiple isolated instances

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

1. `--runtime`
2. `OASCLI_RUNTIME_URL`
3. instance registry (`runtime.json`) for the selected instance
4. fallback to `http://127.0.0.1:8765`

The instance selection itself can come from:

1. `--instance-id`
2. `OASCLI_INSTANCE_ID`
3. derived ID from the config path
4. `default`

If you pass `--state-dir` (or `OASCLI_STATE_DIR`), `oascli` also derives the cache root under that state directory.

## Stale runtime registry fallback

If `runtime.json` points at a runtime URL that no longer answers TCP connections, `oascli`:

1. removes the stale registry file
2. ignores that URL
3. falls back to the default runtime URL

What it does **not** do:

- it does not restart `oasclird`
- it does not search for another daemon automatically
- it does not persist a new URL unless a daemon writes a fresh `runtime.json`

## Security note for remote bindings

`oasclird` has no built-in auth for its own HTTP API. If you bind beyond localhost, use an external reverse proxy, firewall, SSH tunnel, or similar network controls.
