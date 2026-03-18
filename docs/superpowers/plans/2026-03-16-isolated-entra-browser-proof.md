# Isolated Entra Browser Proof Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a net-new Microsoft Entra application in an isolated Terraform workspace, wire it into the Authentik reference broker, and complete the real Authentik -> Entra browser proof without modifying the existing `k8s.oremuslabs.app` deployment.

**Architecture:** Keep the existing `entra-oidc/` Terraform module, but isolate the new state with a dedicated non-default Terraform workspace and a dedicated `tfvars` file for the new `ocli` app. Use the new Entra app only as the Authentik upstream for the browser-login proof, while the already-verified Authentik workload path remains unchanged. Capture live evidence for the browser proof and update the reference docs with the exact isolated setup.

**Tech Stack:** Terraform, Azure Entra ID, Azure CLI, Authentik 2024.12, Go product tests, Docusaurus docs

---

## File structure

### Terraform / Entra app isolation

- Create: `../entra-oidc/envs/ocli-runtime-auth.tfvars`
  - Dedicated app name, redirect URIs, and any claim/group settings for the `ocli` reference proof.
- Modify: `../entra-oidc/README.md`
  - Document the new isolated workspace + tfvars workflow so operators do not apply against the default workspace by accident.

### Authentik/browser proof docs

- Modify: `examples/runtime-auth-broker/authentik/entra-federation.md`
  - Replace generic placeholders with the isolated-workspace workflow and the exact Authentik callback/runtime steps used in the proof.
- Modify: `examples/runtime-auth-broker/authentik/evidence-checklist.md`
  - Mark the required live artifacts for the isolated Entra app proof.
- Modify: `examples/runtime-auth-broker/authentik/README.md`
  - Add the isolated Entra app + workspace commands to the verification section.
- Modify: `product-tests/README.md`
  - Point the manual browser-proof follow-up at the new isolated Entra app workflow.
- Modify: `website/docs/runtime/authentik-reference.md`
  - Describe the isolated Entra app/workspace requirement for the enterprise browser proof.

### Session / proof artifacts (do not commit)

- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/`
  - Store captured outputs such as runtime info, browser config, filtered catalog, and redacted token claims.

---

## Chunk 1: Create the isolated Entra application

### Task 1: Add isolated Terraform inputs and document the safe workflow

**Files:**
- Create: `../entra-oidc/envs/ocli-runtime-auth.tfvars`
- Modify: `../entra-oidc/README.md`

- [x] **Step 1: Write the failing safety check in the README**

Add a short “do not use default workspace” section to `../entra-oidc/README.md` before touching Terraform inputs.

Expected text includes:

```md
- use a non-default Terraform workspace
- use envs/ocli-runtime-auth.tfvars
- do not apply from the existing default workspace
```

- [x] **Step 2: Add the new isolated tfvars file**

Create `../entra-oidc/envs/ocli-runtime-auth.tfvars` with:

```hcl
tenant_id = "abfcbee8-658f-4ab3-97f5-9b357e0f8cda"
app_name  = "ocli-runtime-auth"
identifier_uri = ""
redirect_uris = [
  "https://auth.oremuslabs.app/application/o/authorize/",
  "https://auth.oremuslabs.app/application/o/source/ocli-entra/callback/"
]
public_redirect_uris = [
  "http://127.0.0.1:8787/callback"
]
```

Adjust the Authentik callback URI if the live Authentik source setup proves a different source slug/path.

- [x] **Step 3: Format and inspect the new input file**

Run:

```bash
cd /home/sbuglione/ocli/entra-oidc
terraform fmt envs/ocli-runtime-auth.tfvars
```

Expected: exit code `0`.

- [x] **Step 4: Update the README with the isolated workspace commands**

Add these exact commands:

```bash
cd /home/sbuglione/ocli/entra-oidc
terraform workspace new ocli-runtime-auth || terraform workspace select ocli-runtime-auth
terraform plan -var-file=envs/ocli-runtime-auth.tfvars
terraform apply -var-file=envs/ocli-runtime-auth.tfvars
```

- [ ] **Step 5: Commit**

```bash
git -C /home/sbuglione/ocli/open-cli add docs/superpowers/plans/2026-03-16-isolated-entra-browser-proof.md examples/runtime-auth-broker/authentik/entra-federation.md examples/runtime-auth-broker/authentik/evidence-checklist.md examples/runtime-auth-broker/authentik/README.md product-tests/README.md website/docs/runtime/authentik-reference.md
```

Do **not** commit Terraform state, secrets, or session artifacts.

### Task 2: Create the isolated workspace and apply the new Entra app

**Files:**
- Use: `../entra-oidc/envs/ocli-runtime-auth.tfvars`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/entra-output-redacted.json`

- [x] **Step 1: Select or create the isolated workspace**

Run:

```bash
cd /home/sbuglione/ocli/entra-oidc
terraform workspace new ocli-runtime-auth || terraform workspace select ocli-runtime-auth
terraform workspace show
```

Expected: output `ocli-runtime-auth`.

- [x] **Step 2: Run the plan first**

Run:

```bash
cd /home/sbuglione/ocli/entra-oidc
terraform plan -var-file=envs/ocli-runtime-auth.tfvars
```

Expected: the plan creates a new Entra application/password resources in the isolated workspace and does not propose changes to the default workspace state.

- [x] **Step 3: Apply the isolated Entra app**

Run:

```bash
cd /home/sbuglione/ocli/entra-oidc
terraform apply -auto-approve -var-file=envs/ocli-runtime-auth.tfvars
```

Expected: apply succeeds.

- [x] **Step 4: Capture outputs to session artifacts**

Run:

```bash
cd /home/sbuglione/ocli/entra-oidc
terraform output -json > ~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/entra-output.json
```

Then create a redacted copy that omits or masks `client_secret` before using the values in docs/evidence.

- [x] **Step 5: Verify the new app is separate**

Run:

```bash
cd /home/sbuglione/ocli/entra-oidc
terraform state list
```

Expected: the isolated workspace contains its own `azuread_application.this` and `azuread_application_password.this` resources for the new app, with no need to switch the default workspace.

---

## Chunk 2: Wire Authentik to the isolated Entra app and run the browser proof

### Task 3: Configure the Authentik Entra upstream with the new app

**Files:**
- Modify: `examples/runtime-auth-broker/authentik/entra-federation.md`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/authentik-source-summary.json`

- [x] **Step 1: Write down the live callback URL before creating the Entra source**

Use Authentik source model/API introspection to determine the exact callback URL pattern for the new source slug, then record it in the runbook before finalizing the Entra redirect URIs.

Expected output:

```md
https://auth.oremuslabs.app/application/o/source/<source-slug>/callback/
```

- [x] **Step 2: Create the Authentik Microsoft Entra source**

Use the isolated Entra `application_id`, `client_secret`, and tenant ID to create a new Authentik source dedicated to this proof, for example `ocli-entra`.

Expected fields:

- consumer key = Entra application ID
- consumer secret = Entra client secret
- tenant-specific Entra authorize/token URLs
- profile/userinfo endpoint

- [x] **Step 3: Attach the source to the login flow used by the runtime browser proof**

Ensure the runtime-facing Authentik application/provider offers the new Entra source during login.

- [x] **Step 4: Verify the Authentik -> Entra redirect round trip**

Run a browser login against the Authentik source entrypoint and confirm:

- browser lands on Microsoft login
- sign-in completes
- control returns to Authentik successfully

- [x] **Step 5: Capture a source summary**

Save a non-secret summary of the Authentik source config to:

```bash
~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/authentik-source-summary.json
```

### Task 4: Run the real browser proof and capture evidence

**Files:**
- Modify: `examples/runtime-auth-broker/authentik/evidence-checklist.md`
- Modify: `examples/runtime-auth-broker/authentik/README.md`
- Modify: `product-tests/README.md`
- Modify: `website/docs/runtime/authentik-reference.md`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/01-runtime-info.json`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/02-browser-config.json`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/04-filtered-catalog.json`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/05-allowed-execution.json`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/06-denied-execution.txt`
- Create in session state: `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/08-token-claims-redacted.json`

- [x] **Step 1: Capture runtime metadata**

Run:

```bash
curl -sS "<runtime-url>/v1/runtime/info?config=<path>" > ~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/01-runtime-info.json
curl -sS "<runtime-url>/v1/auth/browser-config?config=<path>" > ~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/files/entra-browser-proof/02-browser-config.json
```

- [x] **Step 2: Execute the real browser login**

Run:

```bash
go run ./cmd/ocli --config <rendered-runtime-config> catalog list --format json
```

Expected: Authentik redirects to Entra, login succeeds, and the command returns the filtered catalog.

- [x] **Step 3: Capture allowed and denied execution**

Run the allowed tool and one denied tool, saving their outputs to the session artifact directory.

- [x] **Step 4: Save redacted token claims**

Export or decode the accepted runtime token and save a redacted claim set containing at least:

- `iss`
- `aud`
- `sub` or `client_id`
- `scope`
- `exp`

- [ ] **Step 5: Update the docs with the isolated app/workspace workflow**

Update the referenced docs so they describe the real isolated Entra app creation path instead of a generic placeholder workflow.

Live proof evidence captured so far:

- `01-runtime-info.json`
- `02-browser-config.json`
- `03-browser-login-success.png`
- `04-filtered-catalog.json`
- `05-allowed-execution.json`
- `06-denied-execution.txt`
- `08-token-claims-redacted.json`

Live proof result summary:

- `ocli catalog list --format json` completed successfully against the isolated Authentik -> Entra browser-login flow.
- The returned catalog only exposed the authorized `tickets:listTickets` tool.
- Direct runtime execution with the same issued token succeeded for `tickets:listTickets` and failed closed with `authz_denied` for `users:listUsers`.
- The accepted Authentik token carried the expected issuer, audience, and scoped authorization envelope claims.

Important follow-up discovered during the live run:

- Authentik rejected the PKCE token exchange while the runtime provider was configured as a confidential client.
- That gap is now closed in source: the Authentik workload fixture renders a dedicated `runtime.oauth-client.cli.json.tmpl` without browser-login metadata, and the browser-facing reference docs now require a separate public provider/config for PKCE browser proof.

- [x] **Step 6: Run verification**

Run:

```bash
cd /home/sbuglione/ocli/open-cli
go test ./cmd/ocli ./internal/runtime ./product-tests/tests -count=1

cd /home/sbuglione/ocli/open-cli/website
npm ci
npm run build
```

Expected:

- Go suites PASS
- docs build prints a successful Docusaurus build

- [ ] **Step 7: Commit**

```bash
cd /home/sbuglione/ocli/open-cli
git add README.md examples/runtime-auth-broker/authentik/README.md examples/runtime-auth-broker/authentik/entra-federation.md examples/runtime-auth-broker/authentik/evidence-checklist.md product-tests/README.md website/docs/runtime/authentik-reference.md website/docs/runtime/deployment-models.md website/docs/security/overview.md product-tests/tests/helpers/authentik_runtime.go product-tests/tests/capability_runtime_auth_authentik_test.go product-tests/tests/capability_auth_policy_test.go
git commit -m "feat: prove isolated Entra browser auth"
```

Use the required co-author trailer. Do not include secrets or session artifacts in the commit.
