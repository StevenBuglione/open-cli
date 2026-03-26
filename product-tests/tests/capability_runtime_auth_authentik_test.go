package tests_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	brokerhelpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCapabilityRuntimeAuthAuthentikOAuthClient(t *testing.T) {
	fixture := brokerhelpers.NewAuthentikRuntimeFixture(t)

	goodToken := fixture.AcquireClientCredentialsToken(t, "bundle:tickets", "tool:tickets:listTickets")

	infoStatus, infoBody := runtimeGet(t, fixture.RuntimeURL, "/v1/runtime/info", fixture.ConfigPath, goodToken)
	if infoStatus != http.StatusOK {
		t.Fatalf("expected 200 runtime info response, got %d with body %s", infoStatus, string(infoBody))
	}
	var info map[string]any
	if err := json.Unmarshal(infoBody, &info); err != nil {
		t.Fatalf("decode runtime info: %v", err)
	}
	auth, ok := info["auth"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth block in runtime info, got %#v", info["auth"])
	}
	if got := auth["required"]; got != true {
		t.Fatalf("expected auth.required=true, got %#v", got)
	}
	if got := auth["audience"]; got != "open-cli-toolbox" {
		t.Fatalf("expected auth audience open-cli-toolbox, got %#v", got)
	}
	if got := auth["principal"]; got == "" || got == nil {
		t.Fatalf("expected resolved principal in runtime info, got %#v", got)
	}
	expectStringSlice(t, auth["tokenValidationProfiles"], []string{"oidc_jwks"}, "auth.tokenValidationProfiles")
	browserLogin, ok := auth["browserLogin"].(map[string]any)
	if !ok {
		t.Fatalf("expected browserLogin block in runtime info, got %#v", auth["browserLogin"])
	}
	if got := browserLogin["configured"]; got != false {
		t.Fatalf("expected oauthClient fixture browserLogin.configured=false, got %#v", got)
	}

	browserStatus, browserBody := runtimeGet(t, fixture.RuntimeURL, "/v1/auth/browser-config", fixture.ConfigPath, "")
	if browserStatus != http.StatusNotFound {
		t.Fatalf("expected 404 browser config response for oauthClient-only fixture, got %d with body %s", browserStatus, string(browserBody))
	}
	if strings.TrimSpace(string(browserBody)) != "browser login is not configured" {
		t.Fatalf("expected browser login not configured body, got %q", strings.TrimSpace(string(browserBody)))
	}

	stdout := runOASCLI(t, fixture, "catalog", "list", "--format", "json")
	if !bytes.Contains(stdout, []byte(`"tickets:listTickets"`)) {
		t.Fatalf("expected oauthClient catalog output to contain tickets:listTickets, got %s", stdout)
	}
	if bytes.Contains(stdout, []byte(`"users:listUsers"`)) {
		t.Fatalf("expected oauthClient catalog output to exclude users:listUsers, got %s", stdout)
	}

	catalogStatus, catalogBody := runtimeGet(t, fixture.RuntimeURL, "/v1/catalog/effective", fixture.ConfigPath, goodToken)
	if catalogStatus != http.StatusOK {
		t.Fatalf("expected 200 effective catalog response, got %d with body %s", catalogStatus, string(catalogBody))
	}
	assertCatalogToolIDs(t, catalogBody, []string{"tickets:listTickets"})

	allowedStatus, allowedBody := runtimeExecute(t, fixture.RuntimeURL, fixture.ConfigPath, goodToken, "tickets:listTickets")
	if allowedStatus != http.StatusOK {
		t.Fatalf("expected 200 for allowed execution, got %d with body %s", allowedStatus, string(allowedBody))
	}

	deniedStatus, deniedBody := runtimeExecute(t, fixture.RuntimeURL, fixture.ConfigPath, goodToken, "users:listUsers")
	if deniedStatus != http.StatusForbidden {
		t.Fatalf("expected 403 for denied execution, got %d with body %s", deniedStatus, string(deniedBody))
	}
	if strings.TrimSpace(string(deniedBody)) != "authz_denied" {
		t.Fatalf("expected authz_denied body for denied execution, got %q", strings.TrimSpace(string(deniedBody)))
	}
}

func TestCapabilityRuntimeAuthAuthentikRejectsInsufficientScope(t *testing.T) {
	fixture := brokerhelpers.NewAuthentikRuntimeFixture(t)

	token := fixture.AcquireClientCredentialsToken(t, "tool:users:listUsers")

	catalogStatus, catalogBody := runtimeGet(t, fixture.RuntimeURL, "/v1/catalog/effective", fixture.ConfigPath, token)
	if catalogStatus != http.StatusOK {
		t.Fatalf("expected 200 effective catalog response, got %d with body %s", catalogStatus, string(catalogBody))
	}
	assertCatalogToolIDs(t, catalogBody, []string{"users:listUsers"})

	status, body := runtimeExecute(t, fixture.RuntimeURL, fixture.ConfigPath, token, "tickets:listTickets")
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for insufficient scope execution, got %d with body %s", status, string(body))
	}
	if strings.TrimSpace(string(body)) != "authz_denied" {
		t.Fatalf("expected authz_denied body for insufficient scope execution, got %q", strings.TrimSpace(string(body)))
	}
}

func TestCapabilityRuntimeAuthAuthentikRejectsWrongAudience(t *testing.T) {
	fixture := brokerhelpers.NewAuthentikRuntimeFixture(t)

	token := fixture.AcquireClientCredentialsToken(t, "bundle:tickets", "tool:tickets:listTickets", "profile:wrong-audience")

	status, body := runtimeExecute(t, fixture.RuntimeURL, fixture.ConfigPath, token, "tickets:listTickets")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong audience token, got %d with body %s", status, string(body))
	}
	if strings.TrimSpace(string(body)) != "authn_failed" {
		t.Fatalf("expected authn_failed body for wrong audience token, got %q", strings.TrimSpace(string(body)))
	}
}

func TestCapabilityRuntimeAuthAuthentikRejectsExpiredToken(t *testing.T) {
	fixture := brokerhelpers.NewAuthentikRuntimeFixture(t)

	token := fixture.AcquireExpiredClientCredentialsToken(t, "bundle:tickets", "tool:tickets:listTickets")

	status, body := runtimeExecute(t, fixture.RuntimeURL, fixture.ConfigPath, token, "tickets:listTickets")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d with body %s", status, string(body))
	}
	if strings.TrimSpace(string(body)) != "authn_failed" {
		t.Fatalf("expected authn_failed body for expired token, got %q", strings.TrimSpace(string(body)))
	}
}

func TestCapabilityRuntimeAuthAuthentikRejectsBadIssuer(t *testing.T) {
	fixture := brokerhelpers.NewAuthentikRuntimeFixture(t)

	token := fixture.AcquireAlternateIssuerToken(t, "bundle:tickets", "tool:tickets:listTickets")

	status, body := runtimeExecute(t, fixture.RuntimeURL, fixture.ConfigPath, token, "tickets:listTickets")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for alternate issuer token, got %d with body %s", status, string(body))
	}
	if strings.TrimSpace(string(body)) != "authn_failed" {
		t.Fatalf("expected authn_failed body for alternate issuer token, got %q", strings.TrimSpace(string(body)))
	}
}

func runtimeGet(t *testing.T, runtimeURL, path, configPath, token string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, runtimeURL+path+"?config="+configPath, nil)
	if err != nil {
		t.Fatalf("new GET request for %s: %v", path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read GET %s response body: %v", path, err)
	}
	return resp.StatusCode, body
}

func runtimeExecute(t *testing.T, runtimeURL, configPath, token, toolID string) (int, []byte) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"configPath": configPath,
		"toolId":     toolID,
	})
	if err != nil {
		t.Fatalf("marshal execute payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, runtimeURL+"/v1/tools/execute", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new execute request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("execute %s: %v", toolID, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read execute response body: %v", err)
	}
	return resp.StatusCode, body
}

func assertCatalogToolIDs(t *testing.T, body []byte, want []string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	catalog, ok := payload["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("expected catalog block, got %#v", payload["catalog"])
	}
	tools, _ := catalog["tools"].([]any)
	got := make([]string, 0, len(tools))
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if id, _ := tool["id"].(string); id != "" {
			got = append(got, id)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected catalog tool ids %v, got %v", want, got)
	}
}

func expectStringSlice(t *testing.T, raw any, want []string, field string) {
	t.Helper()

	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected %s to be an array, got %#v", field, raw)
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		value, _ := item.(string)
		got = append(got, value)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %s=%v, got %v", field, want, got)
	}
}

func runOASCLI(t *testing.T, fixture *brokerhelpers.AuthentikRuntimeFixture, args ...string) []byte {
	t.Helper()

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	cmdArgs := append([]string{"run", "./cmd/ocli", "--config", fixture.ConfigPath}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), fixture.CLIEnv()...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ocli %v failed: %v\n%s", args, err, output)
	}
	return output
}
