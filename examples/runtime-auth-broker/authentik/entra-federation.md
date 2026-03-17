# Entra Federation Runbook for the Authentik Reference Broker

This runbook describes one concrete way to prove the brokered runtime auth contract with **Microsoft Entra ID as the upstream identity provider** and **Authentik as the reference broker**.

This document is intentionally **descriptive, not normative**. `oascli` and `oasclird` only require a broker that satisfies the runtime auth contract. Authentik + Entra is the official worked example in this repository.

## Required operator inputs

Before starting, gather these values:

- Entra tenant ID
- Entra client/application ID
- Entra redirect URI(s)
- Entra group and/or claim values that will drive runtime-scope mapping
- Authentik issuer URL
- runtime audience value (for this reference proof, `oasclird`)
- test user identity that can sign in through Entra

## Contract boundaries

The runtime contract stays the same regardless of the upstream identity provider:

- `oascli` discovers runtime auth requirements from `/v1/runtime/info`
- `oascli` reads browser metadata from `/v1/auth/browser-config`
- the broker issues a runtime token with the required claims
- `oasclird` validates the bearer token with `oidc_jwks`
- authorization is enforced from normalized runtime scopes such as `bundle:*`, `profile:*`, and `tool:*`

Entra federation changes **how Authentik authenticates the user**, not what the runtime expects on the wire.

## Prerequisites

- The Authentik reference deployment is up and reachable
- The Authentik runtime application/provider exists
- The runtime config template has been rendered for your deployment
- You can sign in to the Entra tenant with the test user

See:

- `README.md`
- `runtime.cli.json.tmpl`
- `evidence-checklist.md`

## 1. Register or select the Entra application

Create or reuse an Entra application that Authentik will use as an upstream IdP.

At minimum:

1. Register the application in the target Entra tenant.
2. Add the redirect URI that Authentik will use for the upstream callback.
3. Enable the scopes and claims that Authentik needs for user identity and group resolution.
4. Record the client ID, tenant ID, client secret or certificate, and redirect URI values.

Recommended Entra outputs to preserve for later evidence:

- application overview screenshot/export
- redirect URI list
- configured group claim settings

## 2. Configure the Entra upstream in Authentik

Inside Authentik:

1. Create a new OAuth/OIDC upstream source for Microsoft Entra ID.
2. Enter the Entra tenant ID, client/application ID, and redirect URI values.
3. Supply the Entra client secret or certificate.
4. Configure Authentik to request the user identity and group/role claims you need for runtime authorization.
5. Verify that Authentik can successfully redirect to Entra and complete the upstream callback.

This upstream is only the first half of the chain. `oascli` will still talk to the **Authentik** runtime application, not directly to Entra.

## 3. Configure the Authentik runtime application/provider

Create or update the Authentik application/provider that will mint runtime tokens consumed by `oascli`.

Use these settings as the reference baseline:

1. Create an Authentik OAuth2/OpenID provider for the runtime.
2. Link it to an Authentik application that `oascli` will target.
3. Configure a browser redirect URI such as `http://127.0.0.1:8787/callback`.
4. Configure a signing key so the provider publishes a JWKS.
5. Keep the issuer stable; `oasclird` validates it exactly.

For the workload and browser proof paths, the provider must emit a runtime-compatible `scope` claim. The validated Authentik pattern is to define scope mappings that derive the emitted `scope` claim from `token.scope`.

Example Authentik scope-mapping expression:

```python
audience = "oasclird"
return {
    "scope": " ".join(token.scope),
    "aud": audience,
}
```

If you need separate staging values for negative tests, you can branch on a requested scope:

```python
audience = "wrong-audience" if "profile:wrong-audience" in token.scope else "oasclird"
return {
    "scope": " ".join(token.scope),
    "aud": audience,
}
```

## 4. Map Entra identity signals into normalized runtime scopes

Define how Entra users/groups become runtime authorization.

Recommended shape:

1. Let Entra emit the user and group claims needed by Authentik.
2. Map those Entra claims into Authentik user attributes, groups, or policies.
3. Decide which runtime scopes Authentik will issue for each access class.
4. Keep the runtime-facing scopes normalized to the contract:
   - `bundle:<service>`
   - `profile:<profile>`
   - `tool:<service>:<operation>`
5. Verify that the test user receives the intended runtime scope set after the Entra sign-in completes.

Important: the runtime does **not** understand Entra-specific claim names. Any Entra groups, app roles, or custom claims must be translated into the normalized runtime scope vocabulary before Authentik mints the token.

## 5. Render the runtime config

Render separate configs for the two proof paths. In Authentik, the provider slug determines discovery and JWKS, so a public browser provider and a confidential workload provider should not share one rendered runtime file.

For browser proof, render `runtime.cli.json.tmpl` with the public provider details and keep:

```json
"runtime": {
  "remote": {
    "oauth": {
      "mode": "browserLogin"
    }
  }
}
```

For workload proof, render `runtime.oauth-client.cli.json.tmpl` with the confidential provider details and use:

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

## Browser-login trust chain

The end-to-end browser proof should follow this exact sequence:

1. `oascli` reads `/v1/runtime/info`
2. `oascli` reads `/v1/auth/browser-config`
3. the browser redirects to Authentik
4. Authentik federates to Entra
5. Authentik issues the runtime token
6. `oasclird` validates the token and enforces scopes

If any step is skipped or replaced with manual token injection, the browser proof is incomplete.

## Manual proof steps

Run this sequence against the real deployment:

1. Start the runtime and confirm `/v1/runtime/info` advertises `oidc_jwks`.
2. Run `oascli --config runtime.cli.json catalog list --format json`.
3. Complete the Authentik -> Entra browser login flow.
4. Save the filtered catalog output.
5. Execute one allowed tool and save the output.
6. Attempt one denied tool and save the failure output.
7. Save the final mapping between Entra claims/groups and normalized runtime scopes.

## Evidence and blocking rule

Do **not** claim the Entra proof is complete unless all required artifacts from `evidence-checklist.md` are captured.

If any of these are missing, stop and mark the proof blocked:

- working Entra tenant
- registered Entra application
- Authentik upstream configuration
- test user identity
- captured proof artifacts
