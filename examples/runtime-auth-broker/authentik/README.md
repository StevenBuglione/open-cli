# Authentik Reference Deployment for Brokered Runtime Auth

This directory contains the official reference deployment for the brokered runtime auth contract using **Authentik as the broker**.

Authentik is the worked example, **not** the requirement. Organizations can replace it with any broker or gateway that emits the same external contract expected by `oascli` and `oasclird`.

## What this reference proves

The reference deployment covers two proof paths:

1. **Human path (`browserLogin`)**
   - `oascli` reads `/v1/runtime/info`
   - `oascli` reads `/v1/auth/browser-config`
   - the browser redirects to Authentik
   - Authentik can federate to Microsoft Entra ID
   - Authentik issues the runtime token
   - `oasclird` validates the token and enforces scopes

2. **Workload path (`oauthClient`)**
   - a workload authenticates to Authentik with client credentials
   - Authentik issues a runtime token
   - `oascli` acquires that token before runtime requests
   - `oasclird` validates the token with `oidc_jwks`
   - catalog visibility and execution remain fail-closed

## Validated Authentik facts

These details were verified against the repo-managed Authentik harness:

- discovery lives at `/application/o/<provider-slug>/.well-known/openid-configuration`
- JWKS lives at `/application/o/<provider-slug>/jwks/`
- the authorization endpoint is Authentik’s shared `/application/o/authorize/`
- the token endpoint is Authentik’s shared `/application/o/token/`
- the product-test stack exposes those working endpoints on HTTPS `:9444`
- the runtime-compatible `scope` claim must be emitted from Authentik scope mappings using `token.scope`

Validated scope-mapping expression:

```python
audience = "oasclird"
return {
    "scope": " ".join(token.scope),
    "aud": audience,
}
```

If you need a deliberate negative test audience:

```python
audience = "wrong-audience" if "profile:wrong-audience" in token.scope else "oasclird"
return {
    "scope": " ".join(token.scope),
    "aud": audience,
}
```

## Quick start

### 1. Start the reference stack

```bash
cd examples/runtime-auth-broker/authentik
cp .env.example .env
docker compose up -d
docker compose ps
```

The shipped product-test harness uses a self-signed certificate, so the validated OIDC endpoints are HTTPS-first. In production, use a normal public TLS deployment instead of the harness shortcut.

### 2. Create the Authentik runtime application/provider

For the Authentik reference deployment, treat the two proof paths as **separate provider configurations**:

- **Browser proof** uses a **public** Authentik OAuth2/OpenID provider plus linked application
- **Workload proof** uses a **confidential** Authentik OAuth2/OpenID provider plus linked application

Both providers must emit the same normalized runtime scope vocabulary, but each one has its own provider-specific issuer and JWKS URL in Authentik.

Browser provider requirements:

- redirect URI: `http://127.0.0.1:8787/callback`
- client type: `public`
- signing key: a key that is published through JWKS
- runtime audience: `oasclird`
- runtime scopes: normalized values such as `bundle:tickets` and `tool:tickets:listTickets`

Workload provider requirements:

- client type: `confidential`
- client credentials enabled for client-credentials token acquisition
- signing key: a key that is published through JWKS
- runtime audience: `oasclird`
- runtime scopes: normalized values such as `bundle:tickets` and `tool:tickets:listTickets`

Do **not** assume that you can keep one rendered config and only flip `runtime.remote.oauth.mode`. Authentik discovery and JWKS are provider-specific, so the browser and workload proofs need separate rendered configs when they use different provider types.

### 3. Render the browser runtime template

Render `runtime.cli.json.tmpl` with your **public browser-provider** values:

```bash
export AUTHENTIK_ISSUER="https://auth.example.com/application/o/oascli-runtime/"
export AUTHENTIK_JWKS_URL="https://auth.example.com/application/o/oascli-runtime/jwks/"
export AUTHENTIK_AUTHORIZATION_URL="https://auth.example.com/application/o/authorize/"
export AUTHENTIK_TOKEN_URL="https://auth.example.com/application/o/token/"
export AUTHENTIK_BROWSER_CLIENT_ID="oascli-browser"
export RUNTIME_AUDIENCE="oasclird"
export RUNTIME_URL="https://runtime.example.com"

envsubst < runtime.cli.json.tmpl > runtime.cli.json
```

### 4. Render the workload runtime template

Render `runtime.oauth-client.cli.json.tmpl` with your **confidential workload-provider** values:

```bash
export AUTHENTIK_ISSUER="https://auth.example.com/application/o/oascli-runtime-workload/"
export AUTHENTIK_JWKS_URL="https://auth.example.com/application/o/oascli-runtime-workload/jwks/"
export AUTHENTIK_TOKEN_URL="https://auth.example.com/application/o/token/"
export RUNTIME_AUDIENCE="oasclird"
export RUNTIME_URL="https://runtime.example.com"

envsubst < runtime.oauth-client.cli.json.tmpl > runtime.oauth-client.cli.json
```

Then provide the confidential client credentials out of band:

```bash
export OAS_REMOTE_CLIENT_ID="your-workload-client-id"
export OAS_REMOTE_CLIENT_SECRET="your-workload-client-secret"
```

The rendered workload file should look like this:

```json
"runtime": {
  "server": {
    "auth": {
      "validationProfile": "oidc_jwks",
      "issuer": "https://auth.example.com/application/o/oascli-runtime-workload/",
      "jwksURL": "https://auth.example.com/application/o/oascli-runtime-workload/jwks/",
      "audience": "oasclird"
    }
  },
  "remote": {
    "oauth": {
      "mode": "oauthClient",
      "audience": "oasclird",
      "scopes": ["bundle:tickets", "tool:tickets:listTickets"],
      "client": {
        "tokenURL": "https://auth.example.com/application/o/token/",
        "clientId": { "type": "env", "value": "OAS_REMOTE_CLIENT_ID" },
        "clientSecret": { "type": "env", "value": "OAS_REMOTE_CLIENT_SECRET" }
      }
    }
  }
}
```

### 5. Configure the runtime server

`oasclird` must validate the Authentik-issued runtime tokens:

```json
{
  "runtime": {
    "server": {
      "auth": {
        "validationProfile": "oidc_jwks",
        "issuer": "https://auth.example.com/application/o/oascli-runtime/",
        "jwksURL": "https://auth.example.com/application/o/oascli-runtime/jwks/",
        "audience": "oasclird",
        "authorizationURL": "https://auth.example.com/application/o/authorize/",
        "tokenURL": "https://auth.example.com/application/o/token/",
        "browserClientId": "oascli-browser"
      }
    }
  }
}
```

For the workload proof, use the workload provider issuer/JWKS instead and omit the browser-login fields entirely. The automated product fixture now does exactly that so it does not advertise a browser flow that the confidential workload provider cannot complete.

## Verification commands

### Automated workload proof

```bash
cd product-tests
make authentik-up
make test-runtime-auth-authentik
make authentik-down
```

This automated slice proves:

- runtime info metadata is correct
- `oauthClient` acquires a live Authentik token
- catalog output is scope-filtered
- allowed execution succeeds
- denied execution fails closed
- wrong audience, expired token, and alternate issuer are rejected

### Manual browser proof

Run the browser path after the automated workload proof:

```bash
oascli --config runtime.cli.json catalog list --format json
```

There is no separate `runtime login` command. The first runtime request triggers the browser flow.

Capture artifacts exactly as described in:

- `entra-federation.md`
- `evidence-checklist.md`

## Endpoint readiness checklist

Before claiming the deployment is ready, verify:

- [ ] discovery returns valid JSON
- [ ] discovery includes `jwks_uri`, `authorization_endpoint`, and `token_endpoint`
- [ ] JWKS returns at least one signing key
- [ ] the token endpoint returns a JWT for client credentials
- [ ] the JWT contains `iss`, `aud`, `sub` or `client_id`, `scope`, and `exp`
- [ ] `oasclird` accepts a valid token and rejects invalid ones
- [ ] `/v1/runtime/info` advertises `oidc_jwks`

## Entra federation

Use the runbook in [`entra-federation.md`](./entra-federation.md) for the concrete Authentik -> Entra setup steps.

Do not claim the enterprise browser proof is complete without the artifacts listed in [`evidence-checklist.md`](./evidence-checklist.md).

## Reference contract

This reference deployment keeps the broker-neutral runtime contract intact:

1. the broker issues runtime-compatible tokens
2. the runtime validates those tokens with `oidc_jwks`
3. authorization is derived from normalized runtime scopes
4. failures remain explicit and fail-closed

## Links

- [Runtime Auth Contract](../reference/README.md)
- [Broker Notes](../reference/broker-notes.md)
- Authentik documentation: https://goauthentik.io/docs/
- Microsoft Entra ID: https://learn.microsoft.com/entra/
