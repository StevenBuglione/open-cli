---
title: Quickstart
---

# Quickstart

**Goal:** get a generated command tree running in under 5 minutes with the supported hosted-runtime model.

You will install `ocli`, generate a `.cli.json`, start `open-cli-toolbox`, and then drive it with `ocli`.

:::tip First win
After step 3, you will already have a working catalog. If you only need a first win, stop there and return when you are ready to point at your own API.
:::

## 1. Generate a starter config

Point `ocli init` at any OpenAPI spec to generate a `.cli.json` automatically:

```bash
ocli init https://petstore3.swagger.io/api/v3/openapi.json
```

This creates a `.cli.json` in the current directory. You can also pass a local file path:

```bash
ocli init ./my-api.openapi.yaml
```

:::info Manual config
If you prefer to create the config by hand, see the [Configuration overview](../configuration/overview) for the full schema. A minimal example:

```json title=".cli.json"
{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "runtime": {
    "mode": "remote",
    "remote": {
      "url": "http://127.0.0.1:8765"
    }
  },
  "sources": {
    "ticketsSource": {
      "type": "openapi",
      "uri": "./tickets.openapi.yaml",
      "enabled": true
    }
  },
  "services": {
    "tickets": {
      "source": "ticketsSource",
      "alias": "helpdesk"
    }
  }
}
```
:::

## 2. Start `open-cli-toolbox`

```bash
open-cli-toolbox --config ./.cli.json --addr 127.0.0.1:8765
```

This hosts the runtime locally on loopback, which is the quickest way to evaluate the remote-only contract.

## 3. Inspect the catalog

In another shell:

```bash
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json catalog list --format pretty
```

**What to expect:**

- The response contains a full `catalog` plus the selected `view`.
- Service aliases become top-level command names.
- Operations become tools such as `tickets:listTickets` and `tickets:getTicket`.
- Generated commands are based on `operationId`: `list-tickets` and `get-ticket`.

If you see catalog output, discovery succeeded.

## 4. Inspect tool metadata before executing anything

These are the safest first commands because they do **not** call the upstream API:

```bash
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json tool schema tickets:listTickets --format pretty
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json explain tickets:listTickets --format pretty
```

## 5. Preview the dynamic command tree

```bash
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json helpdesk tickets --help
```

This help renders correctly because the runtime and config are already available. Without a reachable runtime plus config, top-level help can fail before Cobra renders anything — that is expected behavior, not a bug.

## 6. Execute a real tool only when the upstream API is reachable

With the sample config, a dynamic command looks like this:

```bash
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json helpdesk tickets list-tickets --status open
```

This calls the first OpenAPI server URL (`https://api.example.com` in the sample config). Replace it with a real service before expecting a successful response.

## Where to go next

- [Choose your path](./choose-your-path) — pick the runtime model and reading order that fits your goal.
- [Configuration overview](../configuration/overview) — add overlays, skill manifests, and workflows.
- [CLI overview](../cli/overview) — learn the full command tree model.
- [Discovery & Catalog overview](../discovery-catalog/overview) — understand how discovery and normalization work.
