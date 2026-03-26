---
title: Introduction
---

# Introduction

**`open-cli` turns OpenAPI descriptions into a remote-only, policy-aware command surface.** Point `ocli` at a hosted runtime and an OpenAPI file, and it generates a typed CLI — with help text, parameter validation, secret resolution, policy enforcement, and audit logging — without writing glue code.

## Two binaries, two roles

| Binary | Role |
|--------|------|
| **`ocli`** | User-facing client. Connects to the runtime, renders dynamic commands, and sends execution requests. |
| **`open-cli-toolbox`** | Hosted runtime server. Loads config, runs discovery, builds the normalized catalog, resolves secrets, enforces policy, executes upstream HTTP calls, and writes audit events. |

## One supported mental model

`ocli` always needs a reachable `open-cli-toolbox` runtime.

- For local evaluation, host `open-cli-toolbox` on your own machine and point `ocli` at `http://127.0.0.1:8765`.
- For teams and production, host `open-cli-toolbox` centrally, secure it with `runtime.server.auth`, and control access with your network perimeter.
- Embedded mode, demo mode, and auto-promotion to a local runtime are no longer supported.

## Important: the command tree is dynamic

`ocli` does **not** have a static command tree. It fetches the catalog before Cobra renders most help output. In practice that means even `ocli --help` usually needs a reachable runtime plus a usable config path.

A safe first-run pattern is:

```bash
open-cli-toolbox --config ./.cli.json --addr 127.0.0.1:8765

# In another shell:
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json --help
```

## How the pieces fit together

1. `ocli` resolves a runtime URL from `--runtime`, `OCLI_RUNTIME_URL`, or `runtime.remote.url`.
2. `open-cli-toolbox` loads `.cli.json`, merges scope files, and validates the effective config.
3. Discovery resolves API descriptions from OpenAPI documents, service roots, or API catalogs.
4. The catalog builder applies overlays, loads skill manifests and Arazzo workflows, and normalizes tools.
5. `ocli` renders service/group/tool commands from the selected effective view.
6. Tool execution goes back through `open-cli-toolbox` so policy, secrets, retries, cache state, and audit logging stay centralized.

## Who should read which section

| You are… | Start here |
|----------|-----------|
| **New user / first run** | [Installation](./installation) → [Quickstart](./quickstart) |
| **Agent author / end user** | [Choose your path](./choose-your-path) → [CLI overview](../cli/overview) |
| **Operator / platform team** | [Runtime](../runtime/overview) → [Configuration](../configuration/overview) → [Operations](../operations/overview) |
| **Enterprise evaluator** | [Enterprise readiness](../runtime/enterprise-readiness) → [Authentik reference proof](../runtime/authentik-reference) → [Fleet validation](../development/fleet-validation) |
| **Contributor** | [Discovery & Catalog](../discovery-catalog/overview) → [Development](../development/overview) |

## Next steps

1. **[Installation](./installation)** — build or install `ocli` and `open-cli-toolbox`.
2. **[Quickstart](./quickstart)** — start a hosted runtime and inspect a generated command tree.
3. **[Choose your path](./choose-your-path)** — pick the deployment and reading path that fits your goal.
