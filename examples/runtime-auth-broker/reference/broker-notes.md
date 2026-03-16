# Broker notes

This reference broker is a contract example, not a required product choice.

## Upstream federation

The broker may accept identity from:

- Microsoft Entra ID
- Google
- GitHub

Those upstream identities are normalized before runtime token issuance. `oascli` does not need provider-specific logic for each one.

## Runtime token requirements

The broker-issued token must be acceptable to `oasclird` under `validationProfile: "oidc_jwks"` and should include:

```json
{
  "iss": "https://broker.example.com",
  "aud": "oasclird",
  "sub": "github:user-123",
  "scope": "bundle:tickets tool:tickets:listTickets",
  "exp": 1773599999
}
```

Machine actors may use `client_id` in addition to or instead of `sub`, but at least one principal identity must be present.

## Authorization behavior

- `bundle:*` and `profile:*` scopes expand the candidate tool set
- explicit `tool:*` scopes intersect that set
- deny rules still win
- catalog filtering and execution must share the same authorization envelope

## Verification reference

The executable reference fixture used for verification lives in:

- `product-tests/tests/helpers/runtime_auth_broker.go`
- `product-tests/tests/capability_runtime_auth_broker_test.go`
