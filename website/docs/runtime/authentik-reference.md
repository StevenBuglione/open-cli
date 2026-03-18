---
title: Authentik Reference Proof
---

# Authentik Reference Proof

`open-cli` keeps the **runtime auth contract broker-neutral**, but this repository now ships one official, working reference proof built around **Authentik**.

That reference proof covers two paths:

- **automated workload proof** with `oauthClient`
- **operator-run browser proof** with Authentik federating to Microsoft Entra ID

Authentik is the example, not the requirement. Any broker or gateway is acceptable as long as it satisfies the runtime auth contract expected by `ocli` and `oclird`.

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
- `oclird` validates that token with `oidc_jwks`
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
6. `oclird` validates the token and enforces scopes

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
audience = "oclird"
return {
    "scope": " ".join(token.scope),
    "aud": audience,
}
```

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
