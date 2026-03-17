---
title: Introduction
---

# Introduction

**`oas-cli-go` turns OpenAPI descriptions into a local, policy-aware command surface.** Point it at an OpenAPI file, and it generates a typed CLI — with help text, parameter validation, secret resolution, and audit logging — without writing any glue code.

## Two binaries, two roles

| Binary | Role |
|--------|------|
| **`oascli`** | User-facing CLI. Asks the runtime for the effective catalog, renders dynamic commands, and sends execution requests. |
| **`oasclird`** | Runtime daemon. Loads config, runs discovery, builds the normalized catalog, resolves secrets, enforces policy, executes upstream HTTP calls, and writes audit events. |

You can run them in **two modes**:

- **Embedded mode** — `oascli --embedded ...` starts the runtime in-process for a single invocation. No daemon required. **Start here.**
- **Daemon mode** — start `oasclird`, then point `oascli` at it with `--runtime`. Better for repeated use, warm caches, and shared access.

## Important: the command tree is dynamic

`oascli` does **not** have a static command tree. It fetches the catalog before Cobra renders most help output. In practice that means even `oascli --help` usually needs a reachable runtime plus a usable config path. **Always use the embedded flag for first runs:**

```bash
./bin/oascli --embedded --config ./.cli.json --help
```

This behavior is deliberate and is called out throughout these docs.

## How the pieces fit together

1. `oascli` resolves a runtime URL or starts an embedded runtime.
2. The runtime loads `.cli.json`, merges scope files, and validates the effective config.
3. Discovery resolves API descriptions from OpenAPI documents, service roots, or API catalogs.
4. The catalog builder applies overlays, loads skill manifests and Arazzo workflows, and normalizes tools.
5. `oascli` renders service/group/tool commands from the selected effective view.
6. Tool execution goes back through the runtime so policy, secrets, retries, cache state, and audit logging stay centralized.

## Who should read which section

| You are… | Start here |
|----------|-----------|
| **New user / first run** | [Installation](./installation) → [Quickstart](./quickstart) |
| **Agent author / end user** | [Choose your path](./choose-your-path) → [CLI overview](../cli/overview) |
| **Operator / platform team** | [Runtime](../runtime/overview) → [Configuration](../configuration/overview) → [Operations](../operations/overview) |
| **Enterprise evaluator** | [Enterprise readiness](../runtime/enterprise-readiness) → [Authentik reference proof](../runtime/authentik-reference) → [Fleet validation](../development/fleet-validation) |
| **Contributor** | [Discovery & Catalog](../discovery-catalog/overview) → [Development](../development/overview) |

## Next steps

1. **[Installation](./installation)** — build the two binaries (takes ~1 minute).
2. **[Quickstart](./quickstart)** — get a generated command tree running in embedded mode.
3. **[Choose your path](./choose-your-path)** — pick the runtime model and doc route that fits your goal.
