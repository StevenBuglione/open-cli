# Authentik Reference Deployment for Brokered Runtime Auth

This directory contains the official reference implementation for brokered runtime auth using Authentik as the identity broker.

**Important:** This is a reference deployment example. Organizations may use any broker or gateway that satisfies the runtime auth contract. Authentik is illustrative, not normative.

## Architecture

The reference deployment demonstrates the two required proof paths:

1. **Human interactive path (browserLogin):**
   - User authenticates through Authentik
   - Authentik can federate to upstream providers (e.g., Microsoft Entra ID)
   - Authentik issues a runtime-compatible token
   - `oascli` completes the browser login flow
   - `oasclird` validates the token and enforces authorization

2. **Workload path (oauthClient):**
   - Workload authenticates to Authentik using client credentials
   - Authentik issues a runtime-compatible token
   - `oascli` acquires the token via OAuth client flow
   - `oasclird` validates the token and enforces authorization

## Success Looks Like

The reference deployment is verified when:

- ✅ Authentik serves discovery/JWKS/browser/token endpoints
- ✅ `oasclird` validates Authentik-issued runtime tokens with `oidc_jwks` validation profile
- ✅ `oascli` using `oauthClient` can acquire a runtime token and list the filtered catalog
- ✅ `oascli runtime info` reports `auth.required=true` and `tokenValidationProfiles` including `oidc_jwks`
- ✅ Authorized tool execution succeeds
- ✅ Unauthorized tool execution is denied (fail-closed)
- ✅ Browser login flow completes successfully with federated identity

## Required Endpoints

Before running the runtime proof, verify that Authentik exposes these endpoints:

### Discovery and validation
- `/.well-known/openid-configuration` - OpenID Connect discovery document
- `/jwks.json` or equivalent JWKS endpoint (discovered from `.well-known/openid-configuration`)

### Authentication endpoints
- Authorization endpoint - for browser-based login flows
- Token endpoint - for token acquisition and refresh

The exact paths depend on your Authentik application configuration. Authentik typically serves these under:
- `http://localhost:9000/application/o/<provider-slug>/.well-known/openid-configuration`
- `http://localhost:9000/application/o/<provider-slug>/jwks/`
- `http://localhost:9000/application/o/<provider-slug>/authorize/`
- `http://localhost:9000/application/o/<provider-slug>/token/`

## Quick Start

### 1. Prerequisites

- Docker and Docker Compose
- `oascli` and `oasclird` binaries
- (Optional) Microsoft Entra ID tenant for upstream federation

### 2. Initial Setup

Copy the example environment file and configure it:

```bash
cd examples/runtime-auth-broker/authentik
cp .env.example .env
```

Edit `.env` and set secure values for:
- `PG_PASS` - PostgreSQL password
- `AUTHENTIK_SECRET_KEY` - Authentik secret key (generate a long random string)

### 3. Start Authentik

```bash
docker compose up -d
```

Wait for services to become healthy:

```bash
docker compose ps
```

### 4. Configure Authentik

Access Authentik at `http://localhost:9000` and complete the initial setup:

1. Create an admin account
2. Create an OAuth2/OpenID Provider for the runtime
3. Create an Application linked to that provider
4. Configure the following:
   - Client ID for browser login (e.g., `oascli-browser`)
   - Client ID and secret for workload login (e.g., `oascli-workload`)
   - Redirect URIs (e.g., `http://localhost:8787/callback`)
   - Token lifetime and signing key
5. Configure scope mappings:
   - Map user groups/roles to runtime scopes like `bundle:tickets` and `tool:tickets:listTickets`
6. (Optional) Configure upstream federation to Microsoft Entra ID. Detailed Entra federation steps are out of scope for Task 1 and will be added in a later task.

### 5. Generate Runtime Configuration

After configuring Authentik, retrieve the endpoint URLs and update `runtime.cli.json`:

```bash
# Substitute placeholders in the template
export AUTHENTIK_ISSUER="http://localhost:9000/application/o/<your-provider-slug>/"
export AUTHENTIK_JWKS_URL="http://localhost:9000/application/o/<your-provider-slug>/jwks/"
export AUTHENTIK_TOKEN_URL="http://localhost:9000/application/o/<your-provider-slug>/token/"
export AUTHENTIK_AUTHORIZATION_URL="http://localhost:9000/application/o/<your-provider-slug>/authorize/"
export AUTHENTIK_BROWSER_CLIENT_ID="oascli-browser"
export RUNTIME_AUDIENCE="oasclird"
export RUNTIME_URL="http://localhost:8080"

envsubst < runtime.cli.json.tmpl > runtime.cli.json
```

The template includes an example OpenAPI source at `./tickets.openapi.yaml`. Replace that path with a real API description you want to expose through the runtime, or add the referenced file before running catalog or execution commands.

### 6. Start the Runtime

Configure `oasclird` to use Authentik for token validation. Your `oasclird` configuration should include:

```yaml
auth:
  required: true
  validationProfile: oidc_jwks
  issuer: http://localhost:9000/application/o/<your-provider-slug>/
  jwksURL: http://localhost:9000/application/o/<your-provider-slug>/jwks/
  audience: oasclird
```

Start `oasclird`:

```bash
oasclird --config runtime-config.yaml
```

### 7. Verify the Runtime Contract

Check runtime metadata:

```bash
oascli --config runtime.cli.json runtime info
```

Expected output includes:
```json
{
  "auth": {
    "required": true,
    "tokenValidationProfiles": ["oidc_jwks"]
  }
}
```

### 8. Test Browser Login (Human Path)

```bash
oascli --config runtime.cli.json catalog list --format json
```

There is no separate `runtime login` command. The first runtime request triggers the configured browser-login flow. This command should:
1. Open your browser
2. Redirect to Authentik
3. (If configured in a later task) Federate to Entra or another upstream provider
4. Return a runtime token to `oascli`
5. Return the filtered effective catalog
6. Store the token for subsequent commands

### 9. Test Workload Path

Update `runtime.cli.json` to use `oauthClient` mode:

```json
{
  "runtime": {
    "remote": {
      "oauth": {
        "mode": "oauthClient",
        "scopes": ["bundle:tickets", "tool:tickets:listTickets"],
        "client": {
          "tokenURL": "http://localhost:9000/application/o/<your-provider-slug>/token/",
          "clientId": { "type": "env", "value": "OAS_REMOTE_CLIENT_ID" },
          "clientSecret": { "type": "env", "value": "OAS_REMOTE_CLIENT_SECRET" }
        }
      }
    }
  }
}
```

Run a catalog command:

```bash
oascli --config runtime.cli.json catalog list
```

This should:
1. Acquire a token using client credentials
2. Show only authorized tools based on the workload's scopes

### 10. Verify Authorization Enforcement

Test allowed execution against a tool that is both present in your runtime config and granted by the issued runtime scopes. For the example `tickets` service shape, that will typically look like:
```bash
oascli --config runtime.cli.json --format json tickets tickets list-tickets
```

For fail-closed denial, use a config that also exposes a second tool outside the granted scope set and verify that attempting to execute it returns an authorization error. The automated multi-tool proof for that case is added in later tasks of this implementation plan.

## Endpoint Readiness Checklist

Before claiming the reference deployment is working, verify:

- [ ] `curl http://localhost:9000/application/o/<provider-slug>/.well-known/openid-configuration` returns valid JSON
- [ ] Discovery document includes `jwks_uri`, `authorization_endpoint`, `token_endpoint`
- [ ] `curl <jwks_uri>` returns valid JWKS with at least one key
- [ ] Browser can reach authorization endpoint
- [ ] Token endpoint accepts client credentials and returns valid JWT
- [ ] JWT includes required claims: `iss`, `aud`, `sub` or `client_id`, `scope`, `exp`
- [ ] `oasclird` can validate tokens using the JWKS endpoint
- [ ] Runtime metadata reports correct auth configuration

## Troubleshooting

### Authentik not starting
- Check logs: `docker compose logs server`
- Verify PostgreSQL and Redis are healthy: `docker compose ps`
- Ensure `AUTHENTIK_SECRET_KEY` is set in `.env`

### Token validation failing
- Verify issuer URL matches exactly (including trailing slash)
- Check JWKS endpoint is reachable from `oasclird`
- Verify token audience matches runtime expectation
- Check token has not expired
- Verify signing key exists in JWKS

### Authorization not working
- Check scope mappings in Authentik
- Verify user/workload has correct group assignments
- Review `oasclird` logs for authorization decisions
- Confirm runtime policy configuration

### Browser login not working
- Verify redirect URI matches exactly
- Check browser client ID is correct
- Ensure callback port (8787) is available
- Review Authentik logs for authentication errors

## Federation to Microsoft Entra ID

Detailed Microsoft Entra federation instructions are out of scope for Task 1 and will be added in a later task.

## Reference Contract

This deployment satisfies the broker-neutral runtime auth contract by:

1. **Token issuance:** Authentik issues JWT tokens with standard claims
2. **Token validation:** `oasclird` validates tokens using OIDC/JWKS discovery
3. **Authorization:** Runtime derives authorization from normalized scopes
4. **Fail-closed:** Unauthorized requests are denied

Organizations may replace Authentik with any compatible broker as long as the external contract remains the same.

## Links

- [Runtime Auth Contract](../reference/README.md)
- [Broker Notes](../reference/broker-notes.md)
- Authentik Documentation: https://goauthentik.io/docs/
- Microsoft Entra ID: https://learn.microsoft.com/entra/

## Notes

This is a reference deployment for documentation and verification purposes. It is **not** a production-ready configuration. For production use:

- Use HTTPS with valid certificates
- Secure all secrets using a secret management system
- Configure proper network isolation
- Enable audit logging
- Implement backup and recovery procedures
- Review and apply Authentik security hardening guidelines
- Configure appropriate session and token lifetimes
- Implement rate limiting and monitoring
