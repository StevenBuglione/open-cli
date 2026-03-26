# Entra Reference Proof Evidence Checklist

Use this checklist before claiming the Authentik + Entra reference proof is complete.

## Artifact root

Store artifacts under a single directory so multiple operators capture the same shape:

```text
artifacts/runtime-auth/entra-reference/
```

Recommended file names:

- `01-runtime-info.json`
- `02-browser-config.json`
- `03-browser-login-success.png`
- `04-filtered-catalog.json`
- `05-allowed-execution.json`
- `06-denied-execution.txt`
- `07-claim-to-scope-mapping.md`
- `08-token-claims-redacted.json`

## Required artifacts

### 1. Runtime metadata

- [ ] `01-runtime-info.json` captured from `GET /v1/runtime/info`
- [ ] shows `auth.required=true`
- [ ] shows `tokenValidationProfiles` including `oidc_jwks`
- [ ] shows the expected runtime audience

### 2. Browser metadata

- [ ] `02-browser-config.json` captured from `GET /v1/auth/browser-config`
- [ ] includes authorization URL
- [ ] includes token URL
- [ ] includes browser client ID

### 3. Successful browser sign-in

- [ ] `03-browser-login-success.png` (or equivalent recorded artifact) proves the Authentik -> Entra -> Authentik round trip completed
- [ ] operator notes include the Entra tenant/app used for the run

### 4. Filtered catalog output

- [ ] `04-filtered-catalog.json` captured from `ocli --config runtime.cli.json catalog list --format json`
- [ ] output contains only tools allowed by the issued runtime scopes

### 5. Allowed execution

- [ ] `05-allowed-execution.json` captured from a successful command or runtime execution
- [ ] tool is present in the filtered catalog
- [ ] response succeeded without bypassing runtime auth

### 6. Denied execution

- [ ] `06-denied-execution.txt` captured from an out-of-scope execution attempt
- [ ] failure is explicit and fail-closed
- [ ] denied tool is outside the issued runtime scope set

### 7. Claim-to-scope mapping

- [ ] `07-claim-to-scope-mapping.md` explains how Entra groups/claims map to normalized runtime scopes
- [ ] mapping uses contract scope names such as `bundle:*`, `profile:*`, or `tool:*`
- [ ] any Authentik policy or attribute transformation involved is documented

### 8. Token claims

- [ ] `08-token-claims-redacted.json` contains a redacted sample of the issued runtime token claims
- [ ] confirms presence of `iss`, `aud`, `sub` or `client_id`, `scope`, and `exp`
- [ ] secrets or raw bearer tokens are not stored

## Sign-off questions

Do not sign off until each answer is “yes”:

- [ ] Did `ocli` discover auth requirements from the runtime instead of using a hand-crafted token?
- [ ] Did Authentik, not Entra directly, mint the runtime token presented to `open-cli-toolbox`?
- [ ] Did `open-cli-toolbox` validate the token using `oidc_jwks`?
- [ ] Did the allowed and denied examples both go through the live runtime?
- [ ] Are the artifacts stored under the standard directory and file names above?
