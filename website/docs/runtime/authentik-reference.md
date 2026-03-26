---
title: Authentik Reference Proof
---

# Authentik Reference Proof

`open-cli` keeps the **runtime auth contract broker-neutral**, but this repository now ships one official, working reference proof built around **Authentik**.

That reference proof covers two paths:

- **automated workload proof** with `oauthClient`
- **operator-run browser proof** with Authentik federating to Microsoft Entra ID

Authentik is the example, not the requirement. Any broker or gateway is acceptable as long as it satisfies the runtime auth contract expected by `ocli` and `open-cli-toolbox`.

## Proof boundary

| Path | Proof type | What you need |
|---|---|---|
| `oauthClient` automated proof | CI-reproducible | Authentik container only (`make authentik-up`) |
| Browser-login (manual) | Live proof only | Real Authentik instance with a UI |
| Entra federation | Live proof only | Real Entra tenant, application registration, test identity |
| Token revocation | **Not implemented** | No proof path exists; tracked gap |

The automated proof is the reproducible CI artifact. The browser-login and Entra federation paths are documented and exercisable by operators, but they require real external infrastructure — they cannot be run in a CI-only environment.

## What the automated proof verifies

The automated Authentik product test proves that:

- Authentik serves discovery, JWKS, authorization, and token endpoints
- `ocli` can acquire a runtime token through `oauthClient`
- `open-cli-toolbox` validates that token with `oidc_jwks`
- catalog visibility is filtered by runtime scopes
- allowed execution succeeds
- denied execution fails closed
- wrong audience, expired token, and alternate issuer cases are rejected

The implementation lives in:

- `product-tests/tests/helpers/authentik_runtime.go`
- `product-tests/tests/capability_runtime_auth_authentik_test.go`

## What the manual browser proof verifies

The manual proof is the human path:

1. `ocli` discovers runtime auth requirements from `/v1/runtime/info`
2. `ocli` reads `/v1/auth/browser-config`
3. the browser redirects to Authentik
4. Authentik federates to Entra
5. Authentik issues the runtime token
6. `open-cli-toolbox` validates the token and enforces scopes

That proof is documented rather than auto-executed because it requires a real Entra tenant, application, and test identity.

## Reference assets

Use these repo paths as the starting point:

- `examples/runtime-auth-broker/authentik/README.md`
- `examples/runtime-auth-broker/authentik/runtime.cli.json.tmpl`
- `examples/runtime-auth-broker/authentik/runtime.oauth-client.cli.json.tmpl`
- `examples/runtime-auth-broker/authentik/entra-federation.md`
- `examples/runtime-auth-broker/authentik/evidence-checklist.md`
- `product-tests/README.md`

The reference uses **separate Authentik provider configurations** for those paths:

- a **public** provider for `browserLogin`
- a **confidential** provider for `oauthClient`

That split is intentional. Authentik discovery and JWKS are provider-specific, so the workload proof does not advertise browser-login metadata from the confidential fixture.

## Verified Authentik details

The shipped product proof validated these Authentik-specific details:

- discovery and JWKS are provider-specific
- authorization and token issuance use Authentik’s shared OAuth endpoints
- the test stack uses HTTPS with a self-signed certificate
- the runtime-compatible `scope` claim must be emitted from Authentik scope mappings using `token.scope`

Validated scope-mapping expression:

```python
audience = "open-cli-toolbox"
return {
    "scope": " ".join(token.scope),
    "aud": audience,
}
```

## Delegated sub-agent access

Delegated sub-agent access stays on the **broker side** of the runtime boundary.

The current runtime contract does not require a special sub-agent API in `open-cli-toolbox`. Instead, the deployment pattern is:

1. a parent actor already holds a valid runtime token for audience `open-cli-toolbox`
2. a broker or broker-adjacent gateway performs a token-exchange step
3. that broker mints a separate child token for the sub-agent
4. `open-cli-toolbox` validates the child token like any other runtime token and enforces only the child scope set

Important constraints for operators:

- the broker is the delegation boundary; do not treat local config or agent selection as a scope-minting mechanism
- child tokens should be short-lived — minutes, not hours, and materially shorter than the parent token lifetime
- delegation must be subset-only; the child token may keep or reduce scopes, but it must never add `bundle:*`, `profile:*`, or `tool:*` access outside the parent envelope
- lineage claims such as `act`, `delegated_by`, or a delegation ID are recommended for auditability, but authorization still comes only from the child token `scope` claim
- local config, curated mode, managed deny rules, and agent profiles may narrow what the child can see or execute, but they must never expand access beyond the child token scopes

This repository does **not** currently document a first-class CLI UX for requesting delegated child tokens. The implemented reality today is the runtime-facing contract plus the reference broker/token-exchange pattern, not a finished end-user delegation workflow in `ocli`.

## Authentik deployment guidance for delegation

For Authentik-backed deployments, the safest assumption is:

- **Authentik remains the identity-facing broker**
- **delegated token exchange happens in an external broker or gateway layer in front of the runtime-facing contract**

That extra layer can validate the parent runtime token, subset-check the requested runtime scopes, add lineage claims, and mint the short-lived child token that `open-cli-toolbox` already knows how to validate.

Do not assume that Authentik alone should become the full delegation engine for sub-agent execution just because it is the reference broker for the base runtime auth proof. The official proof in this repository covers runtime-compatible token issuance and validation; delegated exchange should be added as a broker/gateway layer without changing the runtime-facing contract.

## Commands

Automated workload proof:

```bash
cd product-tests
make authentik-up
make test-runtime-auth-authentik
make authentik-down
```

Docs verification:

```bash
cd website
npm run build
```

## Related docs

- [Deployment models](./deployment-models)
- [Runtime HTTP API](./http-api)
- [Security overview](../security/overview)
