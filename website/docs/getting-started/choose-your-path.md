---
title: Choose Your Path
---

# Choose Your Path

**Pick the row that matches your goal.** Each path has its own reading order and first commands.

---

## Quick-reference decision table

| Goal | Time investment | Start with |
|------|----------------|------------|
| See it work in 5 minutes | Minimal | [Path 1 — First run](#1-first-successful-run) |
| Host the runtime on your own machine | Low–medium | [Path 2 — Loopback-hosted runtime](#2-loopback-hosted-runtime) |
| Remote runtime with auth | Medium | [Path 3 — Remote runtime auth](#3-remote-runtime-auth-and-scoped-access) |
| MCP integrations | Medium | [Path 4 — MCP](#4-mcp-integrations) |
| Enterprise evaluation | High | [Path 5 — Enterprise](#5-enterprise-readiness-evaluation) |

---

## 1. First successful run

**Choose this if:** You want the smallest possible setup and a working command tree in minutes.

**What you get:** A generated CLI from a sample OpenAPI file, running through a locally hosted `open-cli-toolbox` runtime.

**Reading order:**
1. [Installation](./installation) — install the two binaries
2. [Quickstart](./quickstart) — create a config, start the runtime, run `catalog list`, inspect the generated command tree

**First commands to run:**
```bash
open-cli-toolbox --config ./.cli.json --addr 127.0.0.1:8765
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json catalog list --format pretty
```

**Read this next:** [CLI overview](../cli/overview) to understand the full command model.

---

## 2. Loopback-hosted runtime

**Choose this if:** You are evaluating the supported hosted-runtime model on a laptop or single machine before moving it to shared infrastructure.

**What you get:** `open-cli-toolbox` hosted on loopback; `ocli` still points at it by URL, so the contract matches every other deployment.

**Reading order:**
1. Complete [Path 1](#1-first-successful-run) first
2. [Runtime overview](../runtime/overview)
3. [Deployment models](../runtime/deployment-models)
4. [Operations overview](../operations/overview)

**Read this next:** [Tracing and instances](../operations/tracing-and-instances) to understand state directories, audit paths, and runtime metadata.

---

## 3. Remote runtime auth and scoped access

**Choose this if:** The runtime is hosted separately and access must be authenticated (bearer tokens, scoped catalogs, browser login).

**What you get:** Runtime bearer auth, catalog filtering, and Authentik-based login working together around `open-cli-toolbox`.

**Reading order:**
1. Complete [Path 2](#2-loopback-hosted-runtime) first
2. [Security overview](../security/overview)
3. [Authentik reference proof](../runtime/authentik-reference)

**Read this next:** [Fleet validation](../development/fleet-validation) to see which auth paths are CI-reproducible.

---

## 4. MCP integrations

**Choose this if:** You care about MCP stdio or streamable HTTP servers connecting through `open-cli-toolbox`.

**What you get:** Understanding of which MCP paths are available, which are proven in CI, and how to configure them.

**Reading order:**
1. Complete [Path 2](#2-loopback-hosted-runtime) first
2. [Discovery & Catalog overview](../discovery-catalog/overview)
3. [Deployment models](../runtime/deployment-models)
4. [Fleet validation](../development/fleet-validation) — especially important here, as it shows which MCP paths are CI-reproducible vs. needing live proof

**Read this next:** [Operations overview](../operations/overview) for runtime observability alongside MCP.

---

## 5. Enterprise readiness evaluation

**Choose this if:** You need a review path you can hand to operators, security reviewers, or buyers — covering hosted deployment, proof of functionality, and audit trails.

**What you get:** A structured path through real auth, runtime, validation, and policy evidence.

**Reading order:**
1. [Enterprise readiness](../runtime/enterprise-readiness) — capabilities overview for evaluators
2. [Authentik reference proof](../runtime/authentik-reference) — real auth flow evidence
3. [Fleet validation](../development/fleet-validation) — CI-reproducible end-to-end proof
4. [Security overview](../security/overview) — policy, secrets, and audit logging

**Read this next:** [Deployment models](../runtime/deployment-models) for the supported hosted-runtime topologies.
