---
title: Enterprise Overview
sidebar_label: Overview
---

# Enterprise Overview

This page organizes the evaluation material for the four audiences that typically review `open-cli` before a pilot decision:

- **Platform engineers** — start with [Deployment](#1-deployment) and [Auditability](#4-auditability)
- **Security reviewers** — start with [Authentication](#2-authentication) and [Known gaps](#5-known-gaps)
- **Architecture reviewers** — read all four sections in order, then [External operational requirements](#6-external-operational-requirements)

Nothing here replaces the underlying reference pages. It organizes them into the areas reviewers typically examine and surfaces the proof boundaries and operational requirements you would otherwise have to infer across pages.

For the structured checklist form of this material, see [Adoption Checklist](./adoption-checklist).

---

## 1. Deployment

`open-cli-toolbox` is the only supported runtime deployment target.

| Model | Description | Reference |
|---|---|---|
| Loopback-hosted runtime | Host `open-cli-toolbox` on localhost or a single machine for evaluation. `ocli` still talks to it over HTTP. | [Deployment Models](../runtime/deployment-models) |
| Shared hosted runtime | Host `open-cli-toolbox` for teams, agents, or automation behind a stable URL. | [Deployment Models](../runtime/deployment-models) |
| Brokered enterprise runtime | Put `open-cli-toolbox` behind runtime bearer auth plus network controls. This is the production posture to evaluate. | [Deployment Models](../runtime/deployment-models) |

Multiple isolated instances are supported (`--instance-id`) so different configs, caches, and audit logs remain separated.

---

## 2. Authentication

Runtime auth is opt-in. When you enable `runtime.server.auth`, the hosted runtime validates bearer tokens and applies runtime scopes before it exposes catalog entries or executes tools.

| Capability | Reference |
|---|---|
| Runtime bearer auth and scope filtering | [Runtime Overview](../runtime/overview) |
| Client-credentials and browser-login runtime access | [Deployment Models](../runtime/deployment-models) |
| Authentik as a broker-neutral reference proof | [Authentik Reference Proof](../runtime/authentik-reference) |
| Microsoft Entra as a documented upstream federation | [Authentik Reference Proof](../runtime/authentik-reference) |
| Per-request auth resolution and secret sources | [Auth Resolution](../security/auth-resolution) · [Secret Sources](../security/secret-sources) |
| Policy and execution approval | [Policy and Approval](../security/policy-and-approval) |

The Authentik reference proof page is the primary evidence document for brokered auth. It covers `oauthClient` (automated) and browser-login (manual) proof, with Entra federation documented as a named upstream.

---

## 3. Reproducible proof

Enterprise reviews typically need evidence that claimed capabilities can be exercised reliably, not just narrated.

| Evidence type | How to verify | Reference |
|---|---|---|
| CI-reproducible product paths | `make product-test-fleet` and inspect the generated `rubric.json` / `transcript.log` artifacts | [Fleet Validation](../development/fleet-validation) |
| Runtime auth failure paths | Covered in the `remote-runtime-auth-failures` fleet lane | [Fleet Validation](../development/fleet-validation) |
| Live proof (external IdP, Entra federation) | Documented in `live-proof-matrix.yaml`; requires real infrastructure | [Fleet Validation](../development/fleet-validation) |

The fleet page explains which lanes are CI-safe and which require live external systems. It does not overclaim the CI boundary.

---

## 4. Auditability

Every tool execution and auth event is written to an append-only JSON audit log.

| Capability | Reference |
|---|---|
| Audit log format and event types | [Audit Logging](../operations/audit-logging) |
| Runtime instance isolation | [Tracing and Instances](../operations/tracing-and-instances) |
| Cache refresh behavior | [Cache and Refresh](../operations/cache-and-refresh) |

Current caveats on the audit log: no built-in rotation or retention policy, no server-side filtering on `GET /v1/audit/events`, and `fsync` durability is best-effort.

---

## 5. Known gaps

This section will remain honest about what the project does not yet claim as a solved, reproducible proof path.

- **Token revocation** — expiry, signature validation, issuer/audience checks, and scope enforcement are covered. Revocation and introspection-backed runtime auth are tracked gaps, not solved capabilities.
- **Live identity proof** — browser-login and Entra federation require real external infrastructure to prove. CI cannot cover them. The `live-proof-matrix.yaml` format tracks these as explicit operator-run evidence rather than hiding them.
- **OTEL export** — `obs.Observer` is the extension point but there is no built-in OpenTelemetry exporter or on-disk trace sink today.

See [Enterprise Readiness](../runtime/enterprise-readiness) for the full narrative, including the recommended evaluation sequence.

---

## 6. External operational requirements

The following capabilities are **not provided by `open-cli` itself**. They require operator-owned infrastructure or process. This list is consolidated here so reviewers do not have to infer it across pages.

| Requirement | Why it matters | What you provide |
|---|---|---|
| Token revocation / introspection | Runtime validates token at issuance time. Revoked tokens remain valid until expiry. | External revocation check in your network path, or short expiry windows as mitigation. |
| Audit log rotation and retention | Audit file is append-only with no built-in rotation, retention, or purge policy. | Log rotation tooling (`logrotate`, sidecar, or log forwarder) against the audit path. |
| Network access control to `open-cli-toolbox` | Runtime auth does not replace your network perimeter. | Firewall rules, reverse proxy auth, SSH tunneling, or container/network isolation for hosted deployments. |
| Live identity proof | Authentik and Entra federation require a real tenant, application registration, and test identity. | Own infrastructure for browser-login and federation proof runs. |
| Audit sink / SIEM integration | There is no push exporter. Audit data is available on disk or via `GET /v1/audit/events` (full file, no filtering). | Pull-based log shipper or a sidecar reading the audit file. |
