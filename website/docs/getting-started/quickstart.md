---
title: Quickstart
---

# Quickstart

This quickstart uses **embedded mode** first because it removes one moving part: you do not need a separate daemon just to inspect the catalog.

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

## 3. Inspect the catalog in embedded mode

```bash
./bin/oascli --embedded --config ./.cli.json catalog list --format pretty
```

What to expect:

- the response contains a full `catalog` plus the selected `view`
- service alias `helpdesk` becomes the top-level command name
- operations become tools such as `tickets:listTickets` and `tickets:getTicket`
- because no overlay renamed them, the generated commands are based on `operationId`: `list-tickets` and `get-ticket`

## 4. Ask for tool metadata before you execute anything

```bash
./bin/oascli --embedded --config ./.cli.json tool schema tickets:listTickets --format pretty
./bin/oascli --embedded --config ./.cli.json explain tickets:listTickets --format pretty
```

These are the safest first commands because they do not call the upstream API.

## 5. Preview the dynamic command tree

```bash
./bin/oascli --embedded --config ./.cli.json helpdesk tickets --help
```

That help works because the config and catalog are already available. Without a runtime/config, top-level help can fail before Cobra renders anything.

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

With the sample above, a dynamic command would look like this:

```bash
./bin/oascli --runtime http://127.0.0.1:8765 --config ./.cli.json helpdesk tickets list-tickets --status open
```

That command will call the first OpenAPI server URL (`https://api.example.com` in this sample). Replace it with a real service before expecting a successful response.

## Where to go next

- Add overlays, skill manifests, and workflows in [Configuration overview](../configuration/overview).
- Learn the command tree in [CLI overview](../cli/overview).
- Learn how discovery and normalization work in [Discovery & Catalog overview](../discovery-catalog/overview).
