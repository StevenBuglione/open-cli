---
title: Quickstart
---

# Quickstart

**Goal:** get a generated command tree running in under 5 minutes.

This quickstart uses **embedded mode** because it removes one moving part — you do not need a separate daemon just to inspect the catalog. You will need the binaries from [Installation](./installation) before continuing.

:::tip First win
After step 3, you will have a working generated CLI. **If you only need a first win, stop at step 3** and return when you are ready for tool execution or daemon mode.
:::

## 1. Create a small OpenAPI document

```yaml title="tickets.openapi.yaml"
openapi: 3.1.0
info:
  title: Helpdesk API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      summary: List tickets
      parameters:
        - name: status
          in: query
          schema:
            type: string
      responses:
        "200":
          description: OK
  /tickets/{id}:
    get:
      operationId: getTicket
      tags: [tickets]
      summary: Get ticket
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
```

## 2. Create a minimal config file

```json title=".cli.json"
{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
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

## 3. Inspect the catalog — your first win ✓

```bash
./bin/oascli --embedded --config ./.cli.json catalog list --format pretty
```

**What to expect:**

- The response contains a full `catalog` plus the selected `view`.
- Service alias `helpdesk` becomes the top-level command name.
- Operations become tools such as `tickets:listTickets` and `tickets:getTicket`.
- Generated commands are based on `operationId`: `list-tickets` and `get-ticket`.

If you see catalog output, the binary is working and discovery succeeded. **If you only needed to confirm the setup works, you can stop here.**

## 4. Inspect tool metadata before executing anything

These are the safest first commands because they do **not** call the upstream API:

```bash
./bin/oascli --embedded --config ./.cli.json tool schema tickets:listTickets --format pretty
./bin/oascli --embedded --config ./.cli.json explain tickets:listTickets --format pretty
```

## 5. Preview the dynamic command tree

```bash
./bin/oascli --embedded --config ./.cli.json helpdesk tickets --help
```

This help renders correctly because the runtime and config are already available. Without `--embedded` and a valid config path, top-level help can fail before Cobra renders anything — that is expected behavior, not a bug.

## 6. Start the daemon when you want a reusable runtime

```bash
./bin/oasclird --config ./.cli.json --addr 127.0.0.1:8765
```

In another shell:

```bash
./bin/oascli --runtime http://127.0.0.1:8765 --config ./.cli.json catalog list --format pretty
```

`oasclird` writes instance metadata to `runtime.json`, so later `oascli` commands can resolve the runtime automatically when the instance ID and state directory line up. See [Deployment models](../runtime/deployment-models) and [Tracing and instances](../operations/tracing-and-instances).

## 7. Execute a real tool only when the upstream API is reachable

With the sample config, a dynamic command looks like this:

```bash
./bin/oascli --runtime http://127.0.0.1:8765 --config ./.cli.json helpdesk tickets list-tickets --status open
```

This calls the first OpenAPI server URL (`https://api.example.com` in this sample). Replace it with a real service before expecting a successful response.

## Where to go next

- [Choose your path](./choose-your-path) — pick the runtime model and reading order that fits your goal.
- [Configuration overview](../configuration/overview) — add overlays, skill manifests, and workflows.
- [CLI overview](../cli/overview) — learn the full command tree model.
- [Discovery & Catalog overview](../discovery-catalog/overview) — understand how discovery and normalization work.
