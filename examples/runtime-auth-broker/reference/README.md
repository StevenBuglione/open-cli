# Reference brokered runtime auth example

This example shows one illustrative way to satisfy the brokered runtime auth contract.

It is intentionally not normative:

- organizations may use any broker or gateway they want
- upstream identity can come from Microsoft Entra ID, Google, GitHub, or another provider
- the only requirement is that the runtime-facing contract stays compatible with `oascli`

The reference shape is:

1. upstream login federates into a broker
2. the broker issues a runtime token for audience `oasclird`
3. `oasclird` validates that token with `validationProfile: "oidc_jwks"`
4. runtime scopes such as `bundle:tickets` and `tool:tickets:listTickets` drive catalog filtering and execution authorization

The broker should expose:

- `GET /.well-known/openid-configuration`
- `GET /jwks.json`
- `GET /authorize`
- `POST /token`

The broker-issued runtime token should preserve these normalized claims:

- `iss`
- `aud`
- `sub` or `client_id`
- `scope`
- `exp`

See:

- `runtime.cli.json` for a sample runtime configuration
- `broker-notes.md` for the contract expectations and upstream-normalization notes
- `product-tests/tests/helpers/runtime_auth_broker.go` for executable fixture code used in verification
