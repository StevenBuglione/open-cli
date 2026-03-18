# Brokered Runtime Auth Contract Specification Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Define a portable remote-runtime auth contract that supports brokered login flows and compatible runtime implementations, plus one reference example.

**Architecture:** Introduce a contract-level spec that standardizes runtime token semantics, runtime auth metadata, and validation-profile behavior without forcing one daemon implementation. Anchor it to the existing remote-runtime authz model, then define one reference brokered example that federates third-party IdPs while still issuing runtime-compatible tokens.

**Tech Stack:** Markdown specs under `docs/superpowers/specs`, planning docs under `docs/superpowers/plans`, existing runtime/auth documentation for alignment

---

## Chunk 1: Contract specification

### Task 1: Finalize the brokered auth contract spec

**Files:**
- Modify: `docs/superpowers/specs/2026-03-15-brokered-runtime-auth-contract-design.md`
- Reference: `docs/superpowers/specs/2026-03-15-remote-runtime-authz-policy-design.md`
- Reference: `docs/superpowers/specs/2026-03-15-runtime-deployment-authz-design.md`

- [ ] **Step 1: Reconcile the existing draft with the problem statement**

Write the opening section explaining that introspection-only runtime auth blocks direct portability to providers like Microsoft Entra ID, Google, and GitHub.

- [ ] **Step 2: Define goals and non-goals**

Write explicit goals and non-goals so the spec clearly standardizes:

- the client-visible runtime auth contract
- the runtime token contract
- one reference example

And clearly does **not** standardize:

- one broker product
- one hosted control plane
- one internal RBAC mapping
- every provider-native token format

- [ ] **Step 3: Define the core contract boundary**

State clearly that the standard is the client-visible runtime auth contract, not one daemon-side validation mechanism.

- [ ] **Step 4: Define design principles and compatibility layers**

Write the design-principles section and the three-layer compatibility model:

1. upstream identity providers
2. runtime auth broker / issuer
3. compatible runtime

Make clear that `ocli` interoperates with the broker/runtime contract, not each upstream provider directly.

- [ ] **Step 5: Define the client-visible auth modes**

Document `providedToken`, `oauthClient`, and `browserLogin` as broker-facing modes, and explain what each mode means in a provider-neutral contract.

- [ ] **Step 6: Define the runtime token contract**

Specify required runtime-token semantics:

- issuer
- audience
- principal
- expiry
- runtime scopes

And define the normalized JWT claim model:

```json
{
  "iss": "https://auth.example.com",
  "aud": "oclird",
  "sub": "user-or-session",
  "scope": "bundle:payments tool:users.get"
}
```

- [ ] **Step 7: Define the runtime metadata contract**

Specify what `GET /v1/auth/browser-config` and `GET /v1/runtime/info` must expose for compatibility and diagnostics.

For `GET /v1/auth/browser-config`, require:

- minimum fields
- recommended fields
- rules for `authorizationURL`, `tokenURL`, `clientId`, `audience`, `issuer`, and `tokenValidationProfile`

For `GET /v1/runtime/info`, require:

- explicit auth-required signaling
- expected audience
- recognized scope prefixes
- advertised validation profiles
- browser-login support flag when applicable

- [ ] **Step 8: Define the runtime scope contract**

Write the standardized scope-prefix section for:

- `bundle:<service-id>`
- `tool:<tool-id>`
- `profile:<profile-name>`

And write the portable authorization rules:

- bundle/profile union
- explicit-tool intersection
- deny-rules-win
- configured-tools-only envelope

- [ ] **Step 9: Define allowed validation profiles**

Document:

- `oidc_jwks`
- `oauth2_introspection`
- `external_assertion`

And make clear that implementations may differ internally while preserving the same observable behavior.

Also require the daemon-observable compatibility rules:

- protected endpoints require bearer auth
- invalid/expired/revoked tokens fail with `authn_failed`
- catalog and execution share one authorization envelope
- empty authorized catalog succeeds with an empty list
- no silent local/embedded fallback

For `external_assertion`, require an explicit trust-boundary warning.

- [ ] **Step 10: Define the error, handshake, and configuration-direction sections**

Write:

- error taxonomy expectations
- handshake metadata expectations
- configuration direction for future server auth profiles
- security and portability rules
- the “consequence for current design” section

- [ ] **Step 11: Define the reference example**

Specify one brokered example where Microsoft, Google, and GitHub federation terminates at a broker that issues runtime-compatible tokens validated by the runtime with `oidc_jwks`.

Also require:

- "illustrative, not normative" wording
- preservation of runtime audience/scope semantics
- no product lock-in language

- [ ] **Step 12: Define testing expectations**

Write the testing section with these required verification targets:

- browser login against the reference broker
- non-interactive token acquisition against the reference broker
- remote catalog filtering and execution auth using runtime scopes
- invalid/expired token behavior
- metadata and handshake compatibility
- upstream federation from Microsoft, Google, and GitHub

- [ ] **Step 13: Review the finalized spec chunk**

Run a plan/spec review against `docs/superpowers/specs/2026-03-15-brokered-runtime-auth-contract-design.md` and confirm the contract is complete before committing it.

- [ ] **Step 14: Commit the spec**

```bash
git add docs/superpowers/specs/2026-03-15-brokered-runtime-auth-contract-design.md
git commit -m "docs: define brokered runtime auth contract"
```

## Chunk 2: Future implementation slices

### Task 2: Generalize runtime auth config and schema

**Files:**
- Modify: `pkg/config/types.go`
- Modify: `pkg/config/schema.go`
- Modify: `pkg/config/load.go`
- Modify: `pkg/config/config_test.go`
- Modify: `pkg/config/cli.schema.json`
- Modify: `spec/schemas/cli.schema.json`
- Modify: `website/docs/configuration/config-schema.md`

- [ ] **Step 1: Write the failing config tests**

Add tests in `pkg/config/config_test.go` named:

- `TestLoadEffectiveLoadsRemoteRuntimeOIDCJWKSConfiguration`
- `TestLoadEffectivePreservesRemoteRuntimeOAuth2IntrospectionConfiguration`

These tests should cover a generalized runtime auth shape that accepts:

- `validationProfile: "oidc_jwks"`
- `issuer`
- `jwksURL`
- existing introspection-profile fields
- legacy `mode: "oauth2Introspection"` configs still loading with the same behavior

- [ ] **Step 2: Run config tests to verify they fail**

Run: `go test ./pkg/config -run 'TestLoadEffectiveLoadsRemoteRuntimeOIDCJWKSConfiguration|TestLoadEffectivePreservesRemoteRuntimeOAuth2IntrospectionConfiguration' -count=1`

Expected: FAIL because the new fields and profile are not supported yet.

- [ ] **Step 3: Implement the minimal config model**

Update the config model and both schema files so the generalized auth contract is represented explicitly while preserving backward-compatible support for:

- existing `runtime.server.auth.mode: "oauth2Introspection"` configs during load/defaulting
- existing schema acceptance for legacy configs
- unchanged introspection behavior after normalization to `validationProfile: "oauth2_introspection"`

- [ ] **Step 4: Run config tests to verify they pass**

Run: `go test ./pkg/config -run 'TestLoadEffectiveLoadsRemoteRuntimeOIDCJWKSConfiguration|TestLoadEffectivePreservesRemoteRuntimeOAuth2IntrospectionConfiguration' -count=1`

Expected: PASS

- [ ] **Step 5: Commit the config slice**

```bash
git add pkg/config/types.go pkg/config/schema.go pkg/config/load.go pkg/config/config_test.go pkg/config/cli.schema.json spec/schemas/cli.schema.json website/docs/configuration/config-schema.md
git commit -m "feat: generalize runtime auth config profiles"
```

### Task 3: Add OIDC JWT/JWKS runtime validation

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/runtime/authn_jwks.go`
- Create: `internal/runtime/authn_jwks_test.go`
- Modify: `internal/runtime/server.go`
- Modify: `internal/runtime/server_test.go`

- [ ] **Step 1: Write the failing JWKS validation tests**

Add tests for:

- valid JWT accepted under `validationProfile: "oidc_jwks"`
- wrong audience rejected with `authn_failed`
- expired token rejected with `authn_failed`
- missing runtime scope claim yielding an empty or denied authorization envelope per contract

- [ ] **Step 2: Run runtime tests to verify they fail**

Run: `go test ./internal/runtime -run 'TestServer.*JWKS|TestServer.*JWT' -count=1`

Expected: FAIL because JWKS validation does not exist yet.

- [ ] **Step 3: Implement minimal JWT/JWKS validation**

Choose one JWT/JWKS library, add it to `go.mod` / `go.sum`, create `internal/runtime/authn_jwks.go`, and wire it into `internal/runtime/server.go`.

- [ ] **Step 4: Run runtime tests to verify they pass**

Run: `go test ./internal/runtime -run 'TestServer.*JWKS|TestServer.*JWT' -count=1`

Expected: PASS

- [ ] **Step 5: Commit the JWKS slice**

```bash
git add go.mod go.sum internal/runtime/authn_jwks.go internal/runtime/authn_jwks_test.go internal/runtime/server.go internal/runtime/server_test.go
git commit -m "feat: add jwks runtime token validation"
```

### Task 4: Expand runtime metadata and handshake contract

**Files:**
- Modify: `internal/runtime/server.go`
- Modify: `internal/runtime/server_test.go`
- Modify: `internal/runtime/contract.go`
- Modify: `internal/runtime/contract_test.go`
- Modify: `website/docs/runtime/http-api.md`
- Modify: `website/docs/runtime/overview.md`

- [ ] **Step 1: Write the failing metadata tests**

Add tests covering:

- `GET /v1/auth/browser-config` minimum and recommended fields
- `GET /v1/runtime/info` auth contract block
- validation-profile advertisement and auth-required signaling
- `contractVersion` / `ServerCapabilities` updates for the expanded handshake
- principal or session identity when resolved
- authorization-envelope metadata/version for diagnostics and compatibility checks

Use or add exact tests named:

- `TestServerBrowserConfigIncludesBrokeredAuthMetadata`
- `TestServerRuntimeInfoIncludesBrokeredAuthMetadata`
- `TestRuntimeInfoEndpointReturnsHandshakeInfo`
- `TestRuntimeInfoContractVersionCompatibleWithServerCapabilities`

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime -run 'TestServerBrowserConfigIncludesBrokeredAuthMetadata|TestServerRuntimeInfoIncludesBrokeredAuthMetadata|TestRuntimeInfoEndpointReturnsHandshakeInfo|TestRuntimeInfoContractVersionCompatibleWithServerCapabilities' -count=1`

Expected: FAIL because the new metadata fields are not present yet.

- [ ] **Step 3: Implement the metadata expansion**

Update runtime handlers to emit the contract-level metadata required by the spec. At minimum, make `/v1/runtime/info` advertise:

- `auth.required`
- `auth.audience`
- `auth.scopePrefixes`
- `auth.tokenValidationProfiles`
- `auth.browserLogin`
- principal or session identity when already resolved by the runtime
- authorization-envelope metadata/version used for catalog/execution parity

Also update `CurrentContractVersion` from `1.0` to `1.1` and extend `ServerCapabilities` to advertise the new brokered-auth handshake surface explicitly.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime -run 'TestServerBrowserConfigIncludesBrokeredAuthMetadata|TestServerRuntimeInfoIncludesBrokeredAuthMetadata|TestRuntimeInfoEndpointReturnsHandshakeInfo|TestRuntimeInfoContractVersionCompatibleWithServerCapabilities' -count=1`

Expected: PASS

- [ ] **Step 5: Commit the metadata slice**

```bash
git add internal/runtime/server.go internal/runtime/server_test.go internal/runtime/contract.go internal/runtime/contract_test.go website/docs/runtime/http-api.md website/docs/runtime/overview.md
git commit -m "feat: expose brokered runtime auth metadata"
```

### Task 5: Align client auth flows with the brokered contract

**Files:**
- Modify: `cmd/ocli/main.go`
- Modify: `cmd/ocli/main_test.go`
- Modify: `pkg/auth/oauth.go`
- Modify: `pkg/auth/oauth_test.go`

- [ ] **Step 1: Write the failing client-flow tests**

Add tests for:

- broker-facing `browserLogin` using runtime-provided metadata
- non-interactive broker token acquisition for `oauthClient`
- invalid runtime auth metadata failing closed

- [ ] **Step 2: Run the client tests to verify they fail**

Run: `go test ./cmd/ocli ./pkg/auth -run 'TestRootCommandUsesOAuthClientRemoteRuntimeBearerToken|TestRootCommandUsesRemoteBrowserLoginBearerToken|TestRootCommandCompletesRemoteBrowserLoginAuthorizationCodeFlow|TestResolveOAuthAccessToken.*' -count=1`

Expected: FAIL because the client does not fully align to the generalized contract yet.

- [ ] **Step 3: Implement the minimal client changes**

Update the runtime token acquisition path and auth helpers to honor the contract-level metadata without adding provider-specific logic.

- [ ] **Step 4: Run the client tests to verify they pass**

Run: `go test ./cmd/ocli ./pkg/auth -run 'TestRootCommandUsesOAuthClientRemoteRuntimeBearerToken|TestRootCommandUsesRemoteBrowserLoginBearerToken|TestRootCommandCompletesRemoteBrowserLoginAuthorizationCodeFlow|TestResolveOAuthAccessToken.*' -count=1`

Expected: PASS

- [ ] **Step 5: Commit the client slice**

```bash
git add cmd/ocli/main.go cmd/ocli/main_test.go pkg/auth/oauth.go pkg/auth/oauth_test.go
git commit -m "feat: align client runtime auth flows with broker contract"
```

### Task 6: Add the reference broker example

**Files:**
- Create: `examples/runtime-auth-broker/reference/README.md`
- Create: `examples/runtime-auth-broker/reference/runtime.cli.json`
- Create: `examples/runtime-auth-broker/reference/broker-notes.md`
- Create: `product-tests/tests/capability_runtime_auth_broker_test.go`
- Create: `product-tests/tests/helpers/runtime_auth_broker.go`

- [ ] **Step 1: Write the failing product test**

Add a product test that exercises the reference contract through a runnable broker-shaped fixture:

- token acquisition succeeds
- remote runtime accepts the issued token
- catalog filtering and execution auth honor runtime scopes
- upstream-provider identities are normalized into runtime-compatible token claims without provider-specific client logic

- [ ] **Step 2: Run the product test to verify it fails**

Run: `go test ./product-tests/tests -run TestCapabilityRuntimeAuthBroker -count=1`

Expected: FAIL because the broker example and coverage do not exist yet.

- [ ] **Step 3: Add the reference example assets**

Document:

- what the broker must expose
- how upstream IdPs federate into it
- what runtime-compatible token semantics it must preserve

This deliverable is:

- a documentation-backed reference example under `examples/runtime-auth-broker/reference/`
- a runnable product-test fixture in `product-tests/tests/helpers/runtime_auth_broker.go`
- not a production broker implementation bundled into `ocli`

The fixture contract should be explicit:

- `GET /.well-known/openid-configuration`
- `GET /jwks.json`
- `GET /authorize`
- `POST /token`
- optional fake upstream assertion inputs for Microsoft, Google, and GitHub normalization paths

- [ ] **Step 4: Implement enough test support to pass**

Add the minimal fixtures and harness support needed for the product test to pass in:

- `product-tests/tests/helpers/runtime_auth_broker.go`
- `product-tests/tests/capability_runtime_auth_broker_test.go`

The fixture should stand up:

- a broker-like token issuer endpoint
- browser/client auth metadata endpoints as needed by the test harness
- fake upstream-provider assertions for Microsoft, Google, and GitHub federation paths, normalized before runtime token issuance

The issued runtime JWT should include at least:

- `iss: http://broker.test`
- `aud: oclird`
- `sub: user-or-session`
- `scope: bundle:payments tool:users.get`
- `exp: <future unix timestamp>`

The product test must assert:

- `/v1/auth/browser-config` points the client at the broker fixture
- the runtime accepts the signed token under `oidc_jwks`
- bundle/profile/tool scope enforcement matches catalog filtering and execution behavior
- changing the upstream-provider input does not require any client/provider-specific code path

- [ ] **Step 5: Run the product test to verify it passes**

Run: `go test ./product-tests/tests -run TestCapabilityRuntimeAuthBroker -count=1`

Expected: PASS

- [ ] **Step 6: Commit the reference example slice**

```bash
git add examples/runtime-auth-broker/reference/README.md examples/runtime-auth-broker/reference/runtime.cli.json examples/runtime-auth-broker/reference/broker-notes.md product-tests/tests/helpers/runtime_auth_broker.go product-tests/tests/capability_runtime_auth_broker_test.go
git commit -m "feat: add reference brokered runtime auth example"
```

### Task 7: Full verification and docs completion

**Files:**
- Modify: `README.md`
- Modify: `website/docs/runtime/deployment-models.md`
- Modify: `website/docs/security/overview.md`
- Modify: `website/docs/runtime/http-api.md`
- Modify: `website/docs/runtime/overview.md`
- Modify: `website/docs/configuration/overview.md`

- [ ] **Step 1: Update docs for the finished contract**

Document:

- contract-oriented auth model
- validation profiles
- brokered reference example
- non-provider-specific interoperability rules

- [ ] **Step 2: Run focused verification**

Run:

```bash
go test ./internal/runtime ./cmd/ocli ./pkg/auth ./product-tests/tests -count=1
```

Expected: PASS

- [ ] **Step 3: Run full verification**

Run:

```bash
go test ./...
make verify
cd website && npm run build
```

Expected:

- all Go tests PASS
- `make verify` PASS
- website build PASS

- [ ] **Step 4: Commit the final docs and verification slice**

```bash
git add README.md website/docs/runtime/deployment-models.md website/docs/security/overview.md website/docs/runtime/http-api.md website/docs/runtime/overview.md website/docs/configuration/overview.md
git commit -m "docs: publish brokered runtime auth contract"
```
