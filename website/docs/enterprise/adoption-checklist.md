---
title: Enterprise Adoption Checklist
sidebar_label: Adoption Checklist
---

# Enterprise Adoption Checklist

Use this checklist to track progress through an enterprise evaluation or initial production deployment. Each item links to the reference page where the capability is documented.

This checklist does not replace a security review. It is a structured path through what the repository currently provides and what it explicitly does not.

---

## Phase 0 — Fit assessment

Answer these questions before investing evaluation time. A "no" answer does not mean the project is wrong for you — it means you need a plan to supply what is missing.

| Question | If no |
|---|---|
| Can you host `open-cli-toolbox` as a separate runtime boundary? | `ocli` is remote-only; you need a reachable hosted runtime before the workflow fits. |
| Can you tolerate revocation as a tracked gap rather than a solved capability? | Token revocation is not implemented. Short expiry + network controls are the current mitigation path. |
| Can you supply audit log rotation and retention tooling? | There is no built-in log rotation, retention policy, or push exporter. |
| Do you have infrastructure available for live identity proof (Authentik, Entra, or equivalent)? | Browser-login and federation proof cannot be validated in CI — you need real identity infrastructure. |
| Are you comfortable with no built-in OTEL trace export? | `obs.Observer` is the extension point; there is no production trace sink today. |

If your answers to these questions are acceptable, continue with the phases below.

---

## Phase 1 — Understand the deployment model

- [ ] Read [Deployment Models](../runtime/deployment-models) and choose how you will host the supported remote runtime.
- [ ] Confirm whether multiple isolated instances (`--instance-id`) are needed for your environment.
- [ ] Decide whether `runtime.remote.url` will be configured in `.cli.json`, injected via environment, or passed by automation.

**Reference:** [Deployment Models](../runtime/deployment-models), [Runtime Overview](../runtime/overview)

---

## Phase 2 — Enable and validate runtime auth

- [ ] Enable `runtime.server.auth` on `open-cli-toolbox` with a suitable `validationProfile`.
- [ ] Verify that catalog filtering and execution denial work under authenticated access.
- [ ] Confirm token validation covers issuer, audience, expiry, and signature checks.
- [ ] Document which auth flows are needed: `providedToken`, `oauthClient`, or `browserLogin`.

**Reference:** [Authentik Reference Proof](../runtime/authentik-reference), [Auth Resolution](../security/auth-resolution), [Security Overview](../security/overview)

**Known gap:** Token revocation and introspection-backed runtime auth are not a solved, reproducible proof path. See [Enterprise Readiness](../runtime/enterprise-readiness) for the honest statement.

---

## Phase 3 — Configure policy and secret handling

- [ ] Review scope-based catalog filtering and execution policy under your intended agent profiles.
- [ ] Confirm secret sources (`env:`, vault references) meet your credential-handling requirements.
- [ ] Review [Policy and Approval](../security/policy-and-approval) and determine whether an approval gate is needed.

**Reference:** [Security Overview](../security/overview), [Secret Sources](../security/secret-sources), [Policy and Approval](../security/policy-and-approval)

---

## Phase 4 — Set up audit logging

- [ ] Confirm the default audit log path is accessible and writable in your deployment environment.
- [ ] Decide whether to override `--audit-path` to direct logs to a central path or log aggregator.
- [ ] Verify that `tool_execution`, `authz_denial`, `authenticated_connect`, and `authn_failure` events appear after test runs.
- [ ] Review the audit log caveats: no built-in rotation, no server-side filtering, best-effort fsync durability.

**Reference:** [Audit Logging](../operations/audit-logging), [Tracing and Instances](../operations/tracing-and-instances)

---

## Phase 5 — Run fleet validation

- [ ] Run `make product-test-fleet` from the repo root.
- [ ] Confirm the `rubric.json` and `transcript.log` artifacts for each lane that covers your deployment scenario.
- [ ] For remote runtime auth scenarios, verify the `remote-runtime-oauth-client` lane passes.
- [ ] For browser-login or Entra federation, review `product-tests/testdata/fleet/live-proof-matrix.yaml` and plan live proof execution against your identity infrastructure.

**Reference:** [Fleet Validation](../development/fleet-validation), [Enterprise Readiness](../runtime/enterprise-readiness)

---

## Phase 6 — Review known gaps and open questions

- [ ] Read the **Known gaps** section of [Enterprise Readiness](../runtime/enterprise-readiness).
- [ ] Confirm that revocation is a tracked gap (not a hidden assumption) and decide whether that is acceptable for your risk model.
- [ ] Review the [Audit Logging](../operations/audit-logging) caveats and decide whether supplemental retention or rotation tooling is needed.
- [ ] Capture any live proof runs against your identity provider using the `live-proof-matrix.yaml` format.

---

## Phase 7 — Plan external operational controls

These items are not provided by `open-cli`. Each requires operator-owned infrastructure or process before a production deployment is credible.

- [ ] **Token revocation** — confirm your risk model accepts expiry-based validity windows, or plan an external revocation check in your network path.
- [ ] **Audit log rotation and retention** — confirm `logrotate`, a log sidecar, or a log forwarder is in place against the audit path.
- [ ] **Network access control** — for hosted runtime deployments, confirm firewall rules, reverse proxy auth, or container/network isolation are in place.
- [ ] **Audit SIEM integration** — plan how audit data will reach your SIEM. Pull-based log shipper reading the audit file is the available path today.
- [ ] **Live identity proof** — for browser-login or Entra federation, confirm you have the necessary tenant, application registration, and test identity to run the live proof matrix.

**Reference:** [Enterprise Overview — External operational requirements](./overview#6-external-operational-requirements)
