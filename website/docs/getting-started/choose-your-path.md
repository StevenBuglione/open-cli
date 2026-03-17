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
| Repeated local use, warm cache | Low–medium | [Path 2 — Local daemon](#2-reusable-local-daemon) |
| Remote runtime with auth | Medium | [Path 3 — Remote auth](#3-remote-runtime-auth-and-scoped-access) |
| MCP integrations | Medium | [Path 4 — MCP](#4-mcp-integrations) |
| Enterprise evaluation | High | [Path 5 — Enterprise](#5-enterprise-readiness-evaluation) |

---

## 1. First successful run

**Choose this if:** You want the smallest possible setup and a working command tree in minutes.

**What you get:** A generated CLI from a sample OpenAPI file, running in embedded mode (no daemon).

**Reading order:**
1. [Installation](./installation) — build the two binaries
2. [Quickstart](./quickstart) — create a config, run `catalog list`, inspect the generated command tree

**First commands to run:**
```bash
./bin/oascli --embedded --config ./.cli.json catalog list --format pretty
./bin/oascli --embedded --config ./.cli.json tool schema tickets:listTickets --format pretty
```

**Read this next:** [CLI overview](../cli/overview) to understand the full command model.

---

## 2. Reusable local daemon

**Choose this if:** You expect repeated CLI use, a warmed catalog cache, or multiple commands against the same config.

**What you get:** `oasclird` running as a local control plane; `oascli` points at it by URL.

**Reading order:**
1. Complete [Path 1](#1-first-successful-run) first
2. [Runtime overview](../runtime/overview)
3. [Deployment models](../runtime/deployment-models)
4. [Operations overview](../operations/overview)

**Read this next:** [Tracing and instances](../operations/tracing-and-instances) to understand `runtime.json` instance resolution.

---

## 3. Remote runtime auth and scoped access

**Choose this if:** The runtime is hosted separately and access must be authenticated (bearer tokens, scoped catalogs, browser login).

**What you get:** Runtime bearer auth, catalog filtering, and Authentik-based login working together.

**Reading order:**
1. Complete [Path 2](#2-reusable-local-daemon) first
2. [Security overview](../security/overview)
3. [Authentik reference proof](../runtime/authentik-reference)

**Read this next:** [Fleet validation](../development/fleet-validation) to see which auth paths are CI-reproducible.

---

## 4. MCP integrations

**Choose this if:** You care about MCP stdio or streamable HTTP servers connecting to `oasclird`.

**What you get:** Understanding of which MCP paths are available, which are proven in CI, and how to configure them.

**Reading order:**
1. Complete [Path 2](#2-reusable-local-daemon) first
2. [Discovery & Catalog overview](../discovery-catalog/overview)
3. [Deployment models](../runtime/deployment-models)
4. [Fleet validation](../development/fleet-validation) — especially important here, as it shows which MCP paths are CI-reproducible vs. needing live proof

**Read this next:** [Operations overview](../operations/overview) for runtime observability alongside MCP.

---

## 5. Enterprise readiness evaluation

**Choose this if:** You need a review path you can hand to operators, security reviewers, or buyers — covering safe deployment, proof of functionality, and audit trails.

**What you get:** A structured path through real auth, runtime, validation, and policy evidence.

**Reading order:**
1. [Enterprise readiness](../runtime/enterprise-readiness) — capabilities overview for evaluators
2. [Authentik reference proof](../runtime/authentik-reference) — real auth flow evidence
3. [Fleet validation](../development/fleet-validation) — CI-reproducible end-to-end proof
4. [Security overview](../security/overview) — policy, secrets, and audit logging

**Read this next:** [Deployment models](../runtime/deployment-models) for the deployment architectures available.
