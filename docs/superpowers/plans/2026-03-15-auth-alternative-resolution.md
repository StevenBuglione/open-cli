# OpenAPI Auth Alternative Resolution Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve OpenAPI security OR-of-AND semantics from catalog build through runtime execution so `ocli` chooses the first satisfiable auth alternative, prefers non-interactive auth first, and applies a normalized auth plan without conflicting targets.

**Architecture:** Add a first-class auth-alternatives model to `pkg/catalog`, then add an internal runtime auth resolver that evaluates alternatives in two passes and emits a normalized executor-facing application plan. Keep the rollout low-risk by introducing the new alternatives model alongside the current flat `auth` field for compatibility, but migrate first-party runtime execution to use the alternatives model immediately.

**Tech Stack:** Go, `kin-openapi`, existing `pkg/auth` OAuth helpers, runtime HTTP executor, product tests in `product-tests/tests`

---

## File Structure

**Catalog auth model**

- Modify: `pkg/catalog/types.go`
  - Add `AuthAlternative` and any auth-plan metadata needed on `catalog.Tool`.
  - Keep `Tool.Auth` only as a staged compatibility field if needed during migration.
- Modify: `pkg/catalog/build.go`
  - Replace the flattening `extractAuth(...)` behavior with `extractAuthAlternatives(...)` that preserves source order and requirement-object boundaries.
- Modify: `pkg/catalog/catalog_test.go`
  - Add focused catalog tests for OR-of-AND preservation, scope preservation, and source-order stability.

**Runtime auth resolution**

- Create: `internal/runtime/auth_resolution.go`
  - Own two-pass auth alternative evaluation, conflict detection, attempted-scheme error reporting, and conversion to executor auth application plans.
- Create: `internal/runtime/auth_resolution_test.go`
  - Keep the new resolver logic out of `server_test.go` so the alternative-selection rules stay readable and isolated.
- Modify: `internal/runtime/server.go`
  - Replace the current flat `resolveAuth(...) -> []httpexec.AuthScheme` path with `resolveAuthPlan(...) -> httpexec.AuthApplicationPlan`.

**OAuth interactivity gate**

- Modify: `pkg/auth/oauth.go`
  - Add a resolver option or sibling API that can distinguish “interactive auth required” from ordinary auth failure.
- Modify: `pkg/auth/oauth_test.go`
  - Add tests proving cached/refreshable authorization-code tokens can resolve with interactive auth disabled while uncached flows fail with a sentinel interactive-required error.

**Executor application plan**

- Create: `pkg/exec/auth_plan.go`
  - Define the normalized auth application plan shape used by runtime resolution and HTTP execution.
- Modify: `pkg/exec/exec.go`
  - Apply normalized headers/query/cookies/TLS data without reinterpreting security scheme semantics.
- Modify: `pkg/exec/exec_test.go`
  - Add plan-application tests for mixed header/query auth and single-valued `Authorization`.

**Product verification**

- Modify: `product-tests/tests/capability_auth_policy_test.go`
  - Add end-to-end auth-alternative coverage using the existing runtime + OAuth/product harness instead of inventing a new one.

**Docs (only if serialized catalog output changes)**

- Modify: `website/docs/runtime/overview.md`
  - Document the runtime’s OR-alternative selection order if the public surface changes or the behavior becomes user-visible enough to warrant mention.

## Chunk 1: Preserve and Resolve Auth Alternatives

### Task 1: Preserve OR-of-AND auth structure in the catalog

**Files:**
- Modify: `pkg/catalog/types.go`
- Modify: `pkg/catalog/build.go`
- Test: `pkg/catalog/catalog_test.go`

- [ ] **Step 1: Write the failing catalog test for OR-of-AND preservation**

```go
func TestBuildCatalogPreservesSecurityAlternatives(t *testing.T) {
    tool := buildSingleToolFromOpenAPI(t, `
openapi: 3.0.3
paths:
  /items:
    get:
      operationId: listItems
      security:
        - api_key: []
        - petstore_oauth: [pets.read]
          tenant_header: []
components:
  securitySchemes:
    api_key:
      type: apiKey
      in: query
      name: api_key
    petstore_oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://auth.example.com/token
          scopes:
            pets.read: Read pets
    tenant_header:
      type: apiKey
      in: header
      name: X-Tenant
`)

    if len(tool.AuthAlternatives) != 2 {
        t.Fatalf("expected 2 auth alternatives, got %#v", tool.AuthAlternatives)
    }
    if len(tool.AuthAlternatives[0].Requirements) != 1 {
        t.Fatalf("expected first alternative to keep single api_key scheme")
    }
    if len(tool.AuthAlternatives[1].Requirements) != 2 {
        t.Fatalf("expected second alternative to preserve AND semantics")
    }
}

func buildSingleToolFromOpenAPI(t *testing.T, openapi string) catalog.Tool {
    t.Helper()
    dir := t.TempDir()
    openapiPath := writeFile(t, dir, "auth.openapi.yaml", openapi)
    ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{
        Config: config.Config{
            CLI:  "1.0.0",
            Mode: config.ModeConfig{Default: "discover"},
            Sources: map[string]config.Source{
                "authSource": {Type: "openapi", URI: filepath.ToSlash(openapiPath), Enabled: true},
            },
            Services: map[string]config.Service{
                "protected": {Source: "authSource", Alias: "protected"},
            },
        },
        ProjectDir: dir,
    })
    if err != nil {
        t.Fatalf("catalog.Build: %v", err)
    }
    if len(ntc.Tools) != 1 {
        t.Fatalf("expected exactly one tool, got %#v", ntc.Tools)
    }
    return ntc.Tools[0]
}
```

- [ ] **Step 2: Run the catalog test to verify it fails**

Run: `go test ./pkg/catalog -run TestBuildCatalogPreservesSecurityAlternatives -count=1`

Expected: FAIL because `catalog.Tool` does not yet preserve auth alternatives.

- [ ] **Step 3: Implement the minimal catalog model change**

```go
type Tool struct {
    // ...
    AuthAlternatives []AuthAlternative `json:"authAlternatives,omitempty"`
    Auth             []AuthRequirement `json:"auth,omitempty"` // legacy compatibility
}

type AuthAlternative struct {
    Requirements []AuthRequirement `json:"requirements,omitempty"`
}

type OAuthFlow struct {
    Mode             string `json:"mode"`
    AuthorizationURL string `json:"authorizationUrl,omitempty"`
    TokenURL         string `json:"tokenUrl,omitempty"`
    RefreshURL       string `json:"refreshUrl,omitempty"`
}

func extractAuthAlternatives(document *openapi3.T, operation *openapi3.Operation) []AuthAlternative {
    security := operation.Security
    if security == nil {
        security = &document.Security
    }
    if security == nil {
        return nil
    }

    var alternatives []AuthAlternative
    for _, item := range *security {
        alt := AuthAlternative{}
        for schemeName := range item {
            schemeRef := document.Components.SecuritySchemes[schemeName]
            if schemeRef == nil || schemeRef.Value == nil {
                continue
            }
            alt.Requirements = append(alt.Requirements, AuthRequirement{
                Name:             schemeName,
                Type:             schemeRef.Value.Type,
                Scheme:           schemeRef.Value.Scheme,
                In:               schemeRef.Value.In,
                ParamName:        schemeRef.Value.Name,
                Scopes:           append([]string(nil), item[schemeName]...),
                OAuthFlows:       extractOAuthFlows(schemeRef.Value),
                OpenIDConnectURL: schemeRef.Value.OpenIdConnectUrl,
            })
        }
        if len(alt.Requirements) > 0 {
            alternatives = append(alternatives, alt)
        }
    }
    return alternatives
}

func extractOAuthFlows(scheme *openapi3.SecurityScheme) []OAuthFlow {
    if scheme == nil || scheme.Flows == nil {
        return nil
    }
    var flows []OAuthFlow
    appendFlow := func(mode string, flow *openapi3.OAuthFlow) {
        if flow == nil {
            return
        }
        flows = append(flows, OAuthFlow{
            Mode:             mode,
            AuthorizationURL: flow.AuthorizationURL,
            TokenURL:         flow.TokenURL,
            RefreshURL:       flow.RefreshURL,
        })
    }
    appendFlow("authorizationCode", scheme.Flows.AuthorizationCode)
    appendFlow("clientCredentials", scheme.Flows.ClientCredentials)
    appendFlow("implicit", scheme.Flows.Implicit)
    appendFlow("password", scheme.Flows.Password)
    return flows
}
```

- [ ] **Step 4: Populate the staged compatibility field intentionally**

```go
tool.AuthAlternatives = extractAuthAlternatives(document, operation)
tool.Auth = flattenLegacyAuth(tool.AuthAlternatives)

func flattenLegacyAuth(alts []AuthAlternative) []AuthRequirement {
    seen := map[string]struct{}{}
    var flat []AuthRequirement
    for _, alt := range alts {
        for _, req := range alt.Requirements {
            key := req.Type + "|" + req.Name + "|" + req.In + "|" + req.ParamName + "|" + req.Scheme
            if _, ok := seen[key]; ok {
                continue
            }
            seen[key] = struct{}{}
            flat = append(flat, req)
        }
    }
    return flat
}
```

- [ ] **Step 5: Run the catalog package tests**

Run: `go test ./pkg/catalog -count=1`

Expected: PASS, including the new auth-alternatives test.

- [ ] **Step 6: Commit the catalog-model slice**

```bash
git add pkg/catalog/types.go pkg/catalog/build.go pkg/catalog/catalog_test.go
git commit -m "feat: preserve auth alternatives in catalog"
```

### Task 2: Add a two-pass runtime auth resolver and interactive gate

**Files:**
- Create: `internal/runtime/auth_resolution.go`
- Create: `internal/runtime/auth_resolution_test.go`
- Modify: `internal/runtime/server.go`
- Modify: `pkg/auth/oauth.go`
- Modify: `pkg/auth/oauth_test.go`

- [ ] **Step 1: Write the failing resolver tests first**

```go
func TestResolveAuthPlanPrefersNonInteractiveAlternative(t *testing.T) {
    if err := os.Setenv("AUTH_ALT_API_KEY", "static-secret"); err != nil {
        t.Fatalf("setenv: %v", err)
    }
    t.Cleanup(func() { _ = os.Unsetenv("AUTH_ALT_API_KEY") })
    cfg := config.Config{
        Secrets: map[string]config.Secret{
            "pets.api_key": {Type: "env", Value: "AUTH_ALT_API_KEY"},
        },
    }
    tool := catalog.Tool{
        ServiceID: "pets",
        AuthAlternatives: []catalog.AuthAlternative{
            {
                Requirements: []catalog.AuthRequirement{{
                    Name: "petstore_oauth",
                    Type: "oauth2",
                    Scopes: []string{"pets.read"},
                }},
            },
            {
                Requirements: []catalog.AuthRequirement{{
                    Name: "api_key",
                    Type: "apiKey",
                    In:   "query",
                    ParamName: "api_key",
                }},
            },
        },
    }

    plan, err := resolveAuthPlan(context.Background(), cfg, tool)
    if err != nil {
        t.Fatalf("resolveAuthPlan: %v", err)
    }
    if got := plan.Query["api_key"]; got != "static-secret" {
        t.Fatalf("expected non-interactive api key alternative, got %#v", plan)
    }
}

func TestResolveAuthPlanRejectsAuthorizationConflict(t *testing.T) {
    cfg := config.Config{
        Secrets: map[string]config.Secret{
            "pets.petstore_oauth": {Type: "oauth2", OAuthConfig: config.OAuthConfig{Mode: "clientCredentials"}},
            "pets.basic_auth":     {Type: "env", Value: "BASIC_AUTH_VALUE"},
        },
    }
    tool := catalog.Tool{
        ServiceID: "pets",
        AuthAlternatives: []catalog.AuthAlternative{{
            Requirements: []catalog.AuthRequirement{
                {Name: "petstore_oauth", Type: "oauth2", Scopes: []string{"pets.read"}},
                {Name: "basic_auth", Type: "http", Scheme: "basic"},
            },
        }},
    }

    _, err := resolveAuthPlan(context.Background(), cfg, tool)
    if err == nil || !strings.Contains(err.Error(), "Authorization") {
        t.Fatalf("expected Authorization conflict error, got %v", err)
    }
}
```

- [ ] **Step 2: Write the failing OAuth interactivity tests**

```go
func TestResolveOAuthAccessTokenReturnsInteractiveRequiredWhenAuthorizationCodeNeedsBrowser(t *testing.T) {
    ctx := context.Background()
    client := http.DefaultClient
    policy := config.PolicyConfig{}
    stateDir := t.TempDir()
    secret := config.Secret{
        Type: "oauth2",
        OAuthConfig: config.OAuthConfig{
            Mode: "authorizationCode",
            ClientID: &config.SecretRef{Type: "literal", Value: "browser-client"},
            AuthorizationURL: "https://auth.example.com/authorize",
            TokenURL: "https://auth.example.com/token",
        },
    }
    requirement := catalog.AuthRequirement{
        Name: "petstore_oauth",
        Type: "oauth2",
        OAuthFlows: []catalog.OAuthFlow{{
            Mode: "authorizationCode",
            AuthorizationURL: "https://auth.example.com/authorize",
            TokenURL: "https://auth.example.com/token",
        }},
    }
    _, err := ResolveOAuthAccessTokenWithOptions(ctx, client, policy, secret, requirement, "pets.oauth", stateDir, nil, ResolveOptions{
        Interactive: false,
    })
    if !errors.Is(err, ErrInteractiveRequired) {
        t.Fatalf("expected ErrInteractiveRequired, got %v", err)
    }
}
```

- [ ] **Step 3: Run the focused runtime/auth tests to verify they fail**

Run: `go test ./internal/runtime ./pkg/auth -run 'TestResolveAuthPlan|TestResolveOAuthAccessTokenReturnsInteractiveRequiredWhenAuthorizationCodeNeedsBrowser' -count=1`

Expected: FAIL because the runtime still resolves a flat auth list and OAuth resolution cannot yet signal interactive-required.

- [ ] **Step 4: Add the interactive gate in `pkg/auth/oauth.go`**

```go
var ErrInteractiveRequired = errors.New("interactive auth required")

type ResolveOptions struct {
    Interactive bool
}

func ResolveOAuthAccessTokenWithOptions(
    ctx context.Context,
    httpClient *http.Client,
    policy config.PolicyConfig,
    secret config.Secret,
    requirement catalog.AuthRequirement,
    providerKey,
    stateDir string,
    keychainResolver func(string) (string, error),
    opts ResolveOptions,
) (string, error) {
    // keychainResolver remains threaded through unchanged so existing env/file/exec/osKeychain
    // secret resolution paths do not fork for the new interactive gate.
    // authorizationCode:
    //   - if cached token exists, return it even when opts.Interactive == false
    //   - if refresh can succeed, use it even when opts.Interactive == false
    //   - if a browser/device/user prompt would be required and opts.Interactive == false,
    //     return ErrInteractiveRequired before calling openBrowser
    // clientCredentials/openIdConnect non-interactive paths keep existing behavior
}
```

- [ ] **Step 5: Implement the runtime auth resolver in a new focused file**

```go
type alternativeAttempt struct {
    SchemeNames []string
    Err         error
}

func resolveAuthPlan(ctx context.Context, cfg config.Config, tool catalog.Tool) (httpexec.AuthApplicationPlan, error) {
    attempts := []alternativeAttempt{}
    for _, interactive := range []bool{false, true} {
        for _, alt := range authAlternativesForTool(tool) {
            plan, attempt, ok := tryResolveAlternative(ctx, cfg, tool.ServiceID, alt, interactive)
            if ok {
                return plan, nil
            }
            attempts = append(attempts, attempt)
        }
    }
    return httpexec.AuthApplicationPlan{}, joinAlternativeErrors(attempts)
}

func authAlternativesForTool(tool catalog.Tool) []catalog.AuthAlternative {
    if len(tool.AuthAlternatives) > 0 {
        return tool.AuthAlternatives
    }
    if len(tool.Auth) == 0 {
        return nil
    }
    return []catalog.AuthAlternative{{Requirements: append([]catalog.AuthRequirement(nil), tool.Auth...)}}
}

func tryResolveAlternative(
    ctx context.Context,
    cfg config.Config,
    serviceID string,
    alt catalog.AuthAlternative,
    interactive bool,
) (httpexec.AuthApplicationPlan, alternativeAttempt, bool) {
    plan := httpexec.AuthApplicationPlan{
        Headers: map[string]string{},
        Query:   map[string]string{},
    }
    attempt := alternativeAttempt{}
    for _, req := range alt.Requirements {
        attempt.SchemeNames = append(attempt.SchemeNames, req.Name)
        resolved, target, err := resolveRequirement(ctx, cfg, serviceID, req, interactive)
        if err != nil {
            attempt.Err = err
            return httpexec.AuthApplicationPlan{}, attempt, false
        }
        switch target.Kind {
        case "header":
            if prior, ok := plan.Headers[target.Name]; ok && prior != resolved {
                attempt.Err = fmt.Errorf("Authorization/header conflict for %s", target.Name)
                return httpexec.AuthApplicationPlan{}, attempt, false
            }
            plan.Headers[target.Name] = resolved
        case "query":
            if prior, ok := plan.Query[target.Name]; ok && prior != resolved {
                attempt.Err = fmt.Errorf("query conflict for %s", target.Name)
                return httpexec.AuthApplicationPlan{}, attempt, false
            }
            plan.Query[target.Name] = resolved
        }
    }
    return plan, attempt, true
}
```

- [ ] **Step 6: Wire `server.go` to use the new resolver**

```go
plan, err := resolveAuthPlan(ctx, cfg, tool)
if err != nil {
    return nil, err
}
return httpexec.Execute(ctx, server.client, httpexec.Request{
    Tool:     tool,
    PathArgs: request.PathArgs,
    Flags:    request.Flags,
    Body:     request.Body,
    AuthPlan: plan,
})
```

- [ ] **Step 7: Run the focused runtime/auth tests again**

Run: `go test ./internal/runtime ./pkg/auth -count=1`

Expected: PASS, including:
- non-interactive-first alternative selection
- interactive fallback when needed
- Authorization conflict rejection
- interactive-required sentinel behavior

- [ ] **Step 8: Commit the resolver slice**

```bash
git add internal/runtime/auth_resolution.go internal/runtime/auth_resolution_test.go internal/runtime/server.go pkg/auth/oauth.go pkg/auth/oauth_test.go
git commit -m "feat: resolve auth alternatives in two passes"
```

### Task 3: Convert executor input from auth schemes to auth application plans

**Files:**
- Create: `pkg/exec/auth_plan.go`
- Modify: `pkg/exec/exec.go`
- Modify: `pkg/exec/exec_test.go`

- [ ] **Step 1: Write the failing executor test for normalized plan application**

```go
func TestExecuteAppliesNormalizedAuthPlan(t *testing.T) {
    plan := httpexec.AuthApplicationPlan{
        Headers: map[string]string{"Authorization": "Bearer token-123"},
        Query:   map[string]string{"api_key": "secret"},
    }
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
            t.Fatalf("unexpected auth header: %q", got)
        }
        if got := r.URL.Query().Get("api_key"); got != "secret" {
            t.Fatalf("unexpected api_key query value: %q", got)
        }
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte(`{"ok":true}`))
    }))
    defer server.Close()

    _, err := httpexec.Execute(context.Background(), http.DefaultClient, httpexec.Request{
        BaseURL: server.URL,
        Tool:    catalog.Tool{ID: "pets:list", Method: http.MethodGet, Path: "/items"},
        AuthPlan: plan,
    })
    if err != nil {
        t.Fatalf("Execute: %v", err)
    }
}
```

- [ ] **Step 2: Run the executor test to verify it fails**

Run: `go test ./pkg/exec -run TestExecuteAppliesNormalizedAuthPlan -count=1`

Expected: FAIL because executor requests still consume `[]AuthScheme`.

- [ ] **Step 3: Implement the normalized plan type and application helper**

```go
type AuthApplicationPlan struct {
    Headers       map[string]string `json:"headers,omitempty"`
    Query         map[string]string `json:"query,omitempty"`
    Cookies       map[string]string `json:"cookies,omitempty"`
    TLS           *TLSAuthPlan      `json:"tls,omitempty"`
    TokenMetadata []TokenMetadata   `json:"tokenMetadata,omitempty"`
}

type TLSAuthPlan struct{}

type TokenMetadata struct {
    SchemeName  string `json:"schemeName,omitempty"`
    ProviderKey string `json:"providerKey,omitempty"`
    Type        string `json:"type,omitempty"`
}

func applyAuthPlan(req *http.Request, plan AuthApplicationPlan) {
    for k, v := range plan.Headers { req.Header.Set(k, v) }
    query := req.URL.Query()
    for k, v := range plan.Query { query.Set(k, v) }
    req.URL.RawQuery = query.Encode()
}
```

- [ ] **Step 4: Wire `Execute(...)` to use the plan**

```go
type Request struct {
    // ...
    AuthPlan AuthApplicationPlan `json:"authPlan,omitempty"`
}
```

- [ ] **Step 5: Run the executor package tests**

Run: `go test ./pkg/exec -count=1`

Expected: PASS with the new plan-application test and the migrated request-building tests.

- [ ] **Step 6: Commit the executor slice**

```bash
git add pkg/exec/auth_plan.go pkg/exec/exec.go pkg/exec/exec_test.go
git commit -m "feat: apply normalized auth plans in executor"
```

### Task 4: Add end-to-end coverage for OR selection and interactive fallback

**Files:**
- Modify: `product-tests/tests/capability_auth_policy_test.go`
- Test: `internal/runtime/auth_resolution_test.go`
- Test: `pkg/catalog/catalog_test.go`
- Test: `pkg/exec/exec_test.go`

- [ ] **Step 1: Write the failing product-style test for non-interactive-first selection**

```go
func TestCapabilityAuthResolutionPrefersNonInteractiveAlternative(t *testing.T) {
    dir := t.TempDir()
    if err := os.Setenv("AUTH_ALT_API_KEY", "static-secret"); err != nil {
        t.Fatalf("setenv: %v", err)
    }
    t.Cleanup(func() { _ = os.Unsetenv("AUTH_ALT_API_KEY") })
    var tokenCalls int32
    oauthAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/oauth/token" {
            atomic.AddInt32(&tokenCalls, 1)
        }
        http.NotFound(w, r)
    }))
    defer oauthAPI.Close()

    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/openapi.yaml" {
            _, _ = io.WriteString(w, authAlternativeOpenAPI(r.Host, oauthAPI.URL))
            return
        }
        if r.URL.Path != "/items" {
            http.NotFound(w, r)
            return
        }
        if got := r.URL.Query().Get("api_key"); got != "static-secret" {
            t.Fatalf("expected api_key query auth, got %q", got)
        }
        _ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
    }))
    defer upstream.Close()

    configPath := writeFile(t, dir, ".cli.json", authAlternativeCLIConfig(upstream.URL, oauthAPI.URL, true))
    srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
    runtimeSrv := httptest.NewServer(srv.Handler())
    defer runtimeSrv.Close()

    result := executeTool(t, runtimeSrv.URL, configPath, "protected:listItems", nil)
    if got, ok := result["statusCode"].(float64); !ok || got != 200 {
        t.Fatalf("expected 200, got %v", result)
    }
    if got := atomic.LoadInt32(&tokenCalls); got != 0 {
        t.Fatalf("expected non-interactive alternative to avoid oauth token calls, got %d", got)
    }
}
```

- [ ] **Step 2: Write the failing product-style test for interactive fallback**

```go
func TestCapabilityAuthResolutionFallsBackToInteractiveAlternative(t *testing.T) {
    dir := t.TempDir()
    installFakeBrowserOpener(t, dir) // copy the PATH-based opener approach from cmd/ocli/main_test.go

    oauthAPI := newAuthorizationCodeOAuthServer(t, "interactive-token")
    defer oauthAPI.Close()
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/openapi.yaml" {
            _, _ = io.WriteString(w, authAlternativeOpenAPI(r.Host, oauthAPI.URL))
            return
        }
        if r.URL.Path != "/items" {
            http.NotFound(w, r)
            return
        }
        if got := r.Header.Get("Authorization"); got != "Bearer interactive-token" {
            t.Fatalf("expected bearer token from interactive fallback, got %q", got)
        }
        _ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
    }))
    defer upstream.Close()

    configPath := writeFile(t, dir, ".cli.json", authAlternativeCLIConfig(upstream.URL, oauthAPI.URL, false))
    srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log"), StateDir: filepath.Join(dir, "state")})
    runtimeSrv := httptest.NewServer(srv.Handler())
    defer runtimeSrv.Close()

    result := executeTool(t, runtimeSrv.URL, configPath, "protected:listItems", nil)
    if got, ok := result["statusCode"].(float64); !ok || got != 200 {
        t.Fatalf("expected 200, got %v", result)
    }
}

func authAlternativeCLIConfig(upstreamURL, oauthURL string, includeAPIKey bool) string {
    apiKeySecret := ""
    if includeAPIKey {
        apiKeySecret = `"protected.api_key": {"type":"env","value":"AUTH_ALT_API_KEY"}`
    }
    return `{
      "cli": "1.0.0",
      "mode": { "default": "discover" },
      "sources": {
        "protectedSource": { "type": "openapi", "uri": "` + upstreamURL + `/openapi.yaml", "enabled": true }
      },
      "services": {
        "protected": { "source": "protectedSource", "alias": "protected" }
      },
      "secrets": {
        ` + apiKeySecret + `,
        "protected.petstore_oauth": {
          "type": "oauth2",
          "mode": "authorizationCode",
          "clientId": { "type": "literal", "value": "browser-client" },
          "authorizationURL": "` + oauthURL + `/authorize",
          "tokenURL": "` + oauthURL + `/token"
        }
      }
    }`
}

func authAlternativeOpenAPI(host, oauthURL string) string {
    return `openapi: 3.0.3
info:
  title: Protected API
  version: "1.0.0"
servers:
  - url: http://` + host + `
paths:
  /items:
    get:
      operationId: listItems
      security:
        - api_key: []
        - petstore_oauth: [pets.read]
      responses:
        "200":
          description: OK
components:
  securitySchemes:
    api_key:
      type: apiKey
      in: query
      name: api_key
    petstore_oauth:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: ` + oauthURL + `/authorize
          tokenUrl: ` + oauthURL + `/token
          scopes:
            pets.read: Read pets
`
}

func installFakeBrowserOpener(t *testing.T, dir string) {
    t.Helper()
    openerName := "xdg-open"
    if runtime.GOOS == "darwin" {
        openerName = "open"
    }
    openerPath := filepath.Join(dir, openerName)
    if err := os.WriteFile(openerPath, []byte("#!/bin/sh\ncurl -fsSL \"$1\" >/dev/null\n"), 0o755); err != nil {
        t.Fatalf("write opener: %v", err)
    }
    t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newAuthorizationCodeOAuthServer(t *testing.T, token string) *httptest.Server {
    t.Helper()
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/authorize":
            redirectURI := r.URL.Query().Get("redirect_uri")
            state := r.URL.Query().Get("state")
            http.Redirect(w, r, redirectURI+"?code=interactive-code&state="+url.QueryEscape(state), http.StatusFound)
        case "/token":
            w.Header().Set("Content-Type", "application/json")
            _ = json.NewEncoder(w).Encode(map[string]any{
                "access_token": token,
                "token_type":   "Bearer",
                "expires_in":   3600,
            })
        default:
            http.NotFound(w, r)
        }
    }))
}
```

- [ ] **Step 3: Run only the new end-to-end tests to verify they fail**

Run: `go test ./product-tests/tests -run 'TestCapabilityAuthResolution' -count=1`

Expected: FAIL until runtime selection order and plan application are fully wired.

- [ ] **Step 4: Make the concrete fixes exposed by the product tests**

Modify:
- `internal/runtime/auth_resolution.go` to read `tool.AuthAlternatives` first and fall back to legacy `tool.Auth` only when alternatives are absent
- `pkg/exec/exec.go` to apply both header and query entries from `AuthApplicationPlan`
- `pkg/catalog/build.go` if the product tests reveal document-level `security` fallback is still being flattened or reordered
- `pkg/auth/oauth.go` if non-interactive pass still starts a browser/device flow unexpectedly

- [ ] **Step 5: Run the focused verification set**

Run: `go test ./pkg/catalog ./pkg/auth ./internal/runtime ./pkg/exec ./product-tests/tests -count=1`

Expected: PASS

- [ ] **Step 6: Update docs only if the public catalog/runtime surface changed**

Run: `rg -n "authAlternatives|auth alternative|non-interactive" website/docs`

Expected: If public behavior is now externally visible, update `website/docs/runtime/overview.md`; otherwise leave docs unchanged and document the compatibility choice in the PR/commit message instead.

- [ ] **Step 7: Run final repository verification**

Run: `make verify`

Expected: PASS

- [ ] **Step 8: Build docs site**

Run: `cd website && npm run build`

Expected: PASS

- [ ] **Step 9: Commit the end-to-end slice**

```bash
git add product-tests/tests/capability_auth_policy_test.go internal/runtime/auth_resolution_test.go pkg/catalog/catalog_test.go pkg/exec/exec_test.go website/docs/runtime/overview.md
git commit -m "test: verify auth alternative resolution end to end"
```

## Notes / Guardrails

- Prefer adding `AuthAlternatives` first instead of mutating the serialized shape of `Tool.Auth` in one jump; that keeps the migration understandable and reduces surprise while first-party code is being updated.
- Do not silently merge conflicting header/query targets. Reject the alternative and continue to the next OR option.
- Treat `Authorization` as a single-valued target in all cases.
- For pass 1, “non-interactive” means “do not start browser/device/user-prompt auth”. Cached authorization-code tokens and refresh-token reuse are allowed.
- Reuse the existing OAuth/product harnesses instead of creating bespoke fake auth frameworks.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-03-15-auth-alternative-resolution.md`. Ready to execute?
