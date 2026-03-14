---
title: Introduction
---

# Introduction

`oas-cli-go` turns OpenAPI descriptions into a local, policy-aware command surface.

The repository ships two binaries:

- **`oascli`** builds the user-facing command tree. It asks the runtime for the effective catalog, renders dynamic commands, and sends execution requests.
- **`oasclird`** is the local runtime daemon. It loads configuration, performs discovery, builds the normalized catalog, resolves secrets, enforces policy, executes upstream HTTP calls, and writes audit events.

The two binaries can run in two ways:

- **Daemon mode**: start `oasclird`, then point `oascli` at it with `--runtime` or an instance registry entry.
- **Embedded mode**: run `oascli --embedded ...` and the runtime is created in-process for that one invocation.

## How the pieces fit together

1. `oascli` resolves a runtime URL or starts an embedded runtime.
2. The runtime loads `.cli.json`, merges scope files, and validates the effective config.
3. Discovery resolves API descriptions from explicit OpenAPI documents, service roots, or API catalogs.
4. The catalog builder applies overlays, loads skill manifests and Arazzo workflows, and normalizes tools.
5. `oascli` renders service/group/tool commands from the selected effective view.
6. Tool execution goes back through the runtime so policy, secrets, retries, cache state, and audit logging stay centralized.

## Who should read which section

- **End users / agent authors**: start with [Quickstart](./quickstart), then read the [CLI overview](../cli/overview) and [Tool execution](../cli/tool-execution).
- **Operators**: focus on [Runtime](../runtime/overview), [Configuration](../configuration/overview), [Security](../security/overview), and [Operations](../operations/overview).
- **Contributors**: read [Discovery & Catalog](../discovery-catalog/overview) and [Development](../development/overview) after you know the user-facing model.

## Important implementation nuance

`oascli` does **not** have a static command tree. It fetches the catalog before Cobra renders most help output. In practice that means even `oascli --help` usually needs a reachable runtime plus a usable config path. The safe pattern is:

```bash
./bin/oascli --embedded --config ./.cli.json --help
```

That behavior is deliberate in the current implementation and is called out throughout these docs.

## Next steps

- Build the binaries in [Installation](./installation).
- Create a minimal config in [Quickstart](./quickstart).
- Learn the runtime-backed command model in [CLI overview](../cli/overview).
