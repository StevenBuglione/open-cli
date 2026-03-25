# Reference brokered runtime auth example

This example shows one illustrative way to satisfy the brokered runtime auth contract.

It is intentionally not normative:

- organizations may use any broker or gateway they want
- upstream identity can come from Microsoft Entra ID, Google, GitHub, or another provider
- the only requirement is that the runtime-facing contract stays compatible with `ocli`

The reference shape is:

1. upstream login federates into a broker
2. the broker issues a runtime token for audience `oclird`
3. `oclird` validates that token with `validationProfile: "oidc_jwks"`
4. runtime scopes such as `bundle:tickets` and `tool:tickets:listTickets` drive catalog filtering and execution authorization
5. optional sub-agent delegation happens through the same broker boundary, with the broker minting a shorter-lived child token whose scopes are a subset of the parent runtime token

The broker should expose:

- `GET /.well-known/openid-configuration`
- `GET /jwks.json`
- `GET /authorize`
- `POST /token`

If you support delegated sub-agent execution, the existing `POST /token` surface should also accept a token-exchange style request from the delegating client or orchestration layer. The parent runtime token is presented to the broker, the requested child scope set is subset-checked, and the broker returns another runtime-compatible token for `oclird`.

The broker-issued runtime token should preserve these normalized claims:

- `iss`
- `aud`
- `sub` or `client_id`
- `scope`
- `exp`

Delegated child tokens use the same runtime contract. They may additionally carry lineage claims such as `act`, `delegated_by`, or a delegation ID for broker-side auditability, but `oclird` authorization still comes from the runtime-compatible scope set on the token itself.

See:

- `runtime.cli.json` for a sample runtime configuration
- `broker-notes.md` for the contract expectations and upstream-normalization notes
- `product-tests/tests/helpers/runtime_auth_broker.go` for executable fixture code used in verification
