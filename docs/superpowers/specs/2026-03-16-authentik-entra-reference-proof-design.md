# Authentik Reference Proof with Entra Federation Design

## Problem

The brokered runtime auth contract is implemented and locally verified, including a live broker-shaped smoke using real `ocli` and `oclird` binaries. What is still missing is an enterprise-grade external proof that demonstrates the contract works with a real identity stack, not just an in-process reference broker fixture.

The project needs one official reference deployment that:

- proves the contract can be adopted by real organizations
- preserves the broker-neutral design of `ocli`
- demonstrates strong enterprise integration with a common upstream identity provider
- avoids turning the spec into a product-specific requirement

## Goals

- Keep the runtime auth contract broker-neutral.
- Provide one official, enterprise-grade reference proof deployment.
- Use a real upstream identity provider commonly used by enterprises.
- Verify both human and workload auth paths end to end.
- Document the minimum evidence required before claiming full enterprise E2E verification.

## Non-goals

- Requiring all organizations to use Authentik.
- Making Entra-specific behavior part of the normative runtime contract.
- Publishing multiple official broker examples in this phase.
- Standardizing an organization's internal group-to-scope mapping policy.

## Selected approach

Use a two-layer model:

1. **Normative contract:** `ocli` and compatible runtimes remain broker-neutral.
2. **Official reference proof:** Authentik is the single documented reference broker, with Microsoft Entra ID as the documented upstream federation example.

This gives the project one concrete, enterprise-relevant reference implementation without coupling the contract to a single vendor product.

## Why this is the right solution

### Why not Entra-only

An Entra-only example proves one real provider path, but it does not prove broker portability. It risks teaching users that the reference architecture is "direct Microsoft integration" instead of "provider-neutral runtime contract plus organization-chosen broker."

### Why Authentik as the reference broker

Authentik is a good reference broker because it:

- speaks standard OAuth2/OIDC surfaces
- can federate upstream enterprise identity providers such as Entra
- can issue broker-owned tokens suitable for the runtime contract
- resembles the architecture many organizations will actually deploy

### Why not multiple official brokers right now

Two or more official reference brokers would improve interoperability confidence, but it would also multiply maintenance burden, example drift, and verification cost. One strong reference broker is the right first proof.

## Reference architecture

The official proof architecture is:

1. A user or workload authenticates against **Microsoft Entra ID**.
2. **Authentik** federates that upstream identity and applies organization-defined policy.
3. Authentik issues a **runtime-compatible token** for audience `oclird`.
4. `ocli` acquires that token through the standard runtime contract.
5. `oclird` validates the token using the configured validation profile, expected to be `oidc_jwks` for the reference deployment.
6. The runtime derives the authorization envelope from normalized runtime scopes and enforces catalog filtering plus execution denial.

For clarity, the reference deployment uses **two trust chains**, not one:

- **Human chain:** Entra authenticates the user, Authentik federates that identity, then Authentik issues the runtime token.
- **Workload chain:** the workload authenticates directly to Authentik using operator-managed client credentials, then Authentik issues the runtime token.

## Required proof paths

The official reference proof must verify two equally required end-to-end paths.

### 1. Human interactive path

This path proves that a real user can authenticate through the standard runtime browser flow:

- `ocli` reads runtime metadata from `GET /v1/runtime/info`
- `ocli` reads browser-login metadata from `GET /v1/auth/browser-config`
- `ocli` completes `browserLogin` with authorization-code + PKCE
- Authentik federates the login to Entra
- Authentik returns a runtime-compatible token
- `oclird` validates the token and enforces runtime authz

### 2. Workload path

This path proves that non-interactive service access works as well:

- `ocli` uses `runtime.remote.oauth.mode: "oauthClient"`
- Authentik is the reference **runtime token issuer** for the workload path as well
- the token endpoint used by `ocli` is therefore Authentik's token endpoint or an Authentik-owned endpoint with equivalent behavior
- the resulting runtime token satisfies the same audience, expiry, identity, and scope semantics
- `oclird` validates the token and enforces the same authorization envelope behavior

For the reference deployment, the workload path does **not** rely on handing raw Entra access tokens directly to `oclird`. Entra is the upstream identity provider for the human path, while Authentik remains the runtime-token broker visible to `ocli` in both paths.

### Workload credential lifecycle and failure behavior

For the reference deployment:

- workload credentials presented to Authentik are Authentik-managed operator credentials for the runtime client
- Authentik is responsible for issuing short-lived runtime-compatible access tokens
- `ocli` consumes those tokens through the existing `oauthClient` path
- `oclird` must fail closed when the issuer, JWKS, audience, expiry, or scopes are invalid
- verification must include at least one negative case showing that an invalid or insufficient workload token is rejected

The reference proof does **not** require Entra-backed workload credentials in phase one. Entra federation is required for the human interactive path; the workload path is required to prove non-interactive runtime-token issuance and validation through the same broker, but it uses Authentik-managed client credentials as the documented reference.

## Completion bar for "enterprise end-to-end verified"

The project should not claim full enterprise E2E verification until all of the following are captured for the reference deployment.

### Deployment evidence

- a real Authentik deployment exists
- a real Entra application registration exists
- Entra-to-Authentik federation is configured and documented
- Authentik is configured to issue runtime-compatible tokens
- `oclird` is configured to validate those tokens using discovery/JWKS

### Human-flow evidence

- a real browser-based sign-in completes successfully
- `ocli` obtains the token using the runtime metadata contract
- authenticated catalog output shows only authorized tools
- an authorized tool execution succeeds
- an unauthorized tool execution is denied

### Workload-flow evidence

- a real non-interactive client obtains a runtime token from the documented workload path
- authenticated catalog output shows only authorized tools
- an authorized tool execution succeeds
- an unauthorized tool execution is denied

### Documentation evidence

- operator setup docs exist for the Authentik reference deployment
- Entra federation steps are documented
- claim-to-runtime-scope mapping is documented
- "bring your own broker" guidance makes clear that Authentik is illustrative, not normative

## Contract boundaries

The spec remains clear on these boundaries:

- **Normative:** runtime metadata contract, runtime token semantics, authorization behavior, fail-closed expectations
- **Illustrative:** Authentik deployment shape, Entra federation wiring, claim mapping examples

Organizations may replace Authentik with another broker, gateway, or compatible custom implementation as long as they preserve the external contract expected by `ocli`.

## Ownership and interface boundaries

To keep the example understandable and replaceable, the reference deployment is split into these units:

### 1. Upstream identity unit: Entra

- authenticates the human principal for the interactive reference path
- remains provider-specific and illustrative
- is not directly consumed by `ocli`

For the reference deployment, this unit is required for the **human** path and not required for the workload path.

### 2. Broker unit: Authentik

- federates Entra identity
- maps upstream identity into normalized runtime claims and scopes
- exposes the browser and token endpoints consumed by `ocli`
- issues the runtime-compatible token used at the runtime boundary

### 3. Client unit: `ocli`

- consumes only the standard runtime metadata and broker-facing OAuth surfaces
- must not require Entra-specific logic
- must behave the same way with another compatible broker

### 4. Runtime unit: `oclird`

- validates the runtime token according to the configured validation profile
- derives the authorization envelope from normalized runtime scopes
- enforces catalog filtering and fail-closed execution denial

The normalized claim-to-scope mapping is owned by the broker unit, while the runtime only depends on the normalized claims it receives.

## Reference deployment requirements

The official Authentik example should include:

- an example runtime config using `runtime.server.auth.validationProfile: "oidc_jwks"`
- a documented Authentik application/provider setup
- documented Entra upstream federation steps
- example user-to-scope and workload-to-scope mappings
- a verification runbook for both browser and workload flows

## What is already proven vs. still missing

### Already proven

- the broker-neutral contract is implemented
- `ocli` and `oclird` interoperate with a live broker-shaped OIDC/JWKS issuer
- the runtime enforces catalog filtering and fail-closed authorization
- the project has one executable reference broker fixture

### Still missing

- real Authentik deployment proof
- real Entra federation proof
- real browser-login proof against an external IdP chain
- real workload-token proof against the documented Authentik workload path
- operator runbooks and setup docs for the reference deployment

## Testing strategy

Testing for the official proof should be split into three layers:

1. **Contract tests**
   - existing unit/product coverage
   - local reference broker smoke

2. **Reference deployment verification**
   - Authentik + Entra browserLogin proof
   - Authentik workload-token proof

3. **Operator documentation verification**
   - follow-the-docs runbook validation on a clean environment

## Acceptance criteria and proof format

The reference proof is complete only when all of the following are produced from a clean environment:

### Environment assumptions

- one reachable Authentik deployment
- one reachable Entra tenant and application registration
- one runtime configured for `oidc_jwks`
- one test principal for browser login
- one test workload credential for `oauthClient`

### Required captured evidence

- exported runtime config used for the proof
- Authentik configuration summary sufficient to reproduce endpoints and claim mapping
- Entra federation configuration summary sufficient to reproduce upstream login
- command transcript or equivalent captured output for the browser path
- command transcript or equivalent captured output for the workload path
- captured deny-case output for at least one unauthorized tool or insufficient-scope token

### Pass criteria

- browser flow succeeds and reaches an allowed tool execution
- workload flow succeeds and reaches an allowed tool execution
- both flows show filtered catalog behavior consistent with the granted scopes
- both flows show fail-closed denial for an unauthorized tool or insufficient scopes
- runtime handshake metadata matches the documented contract
- no step depends on provider-specific client logic inside `ocli`

## Risks and mitigations

### Risk: reference example becomes mistaken for the only supported architecture

Mitigation:

- repeatedly label Authentik as the reference implementation, not the required product
- keep the normative contract sections separate from deployment-example sections

### Risk: Entra-specific claims leak into the contract

Mitigation:

- keep provider-specific claim handling inside the illustrative broker mapping layer
- document normalized runtime claims separately from upstream claims

### Risk: example drift

Mitigation:

- require a verification runbook
- require reproducible evidence for both human and workload paths
- keep the example narrow: one broker, one upstream provider, two required flows

## Decision

The correct solution is:

- keep the **spec broker-neutral**
- adopt **Authentik as the single official reference broker**
- document **Microsoft Entra ID as the upstream enterprise federation path**
- require both **browserLogin** and **oauthClient** to pass real external end-to-end verification before claiming full enterprise proof
