package tests_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignRemoteRuntimeFailures(t *testing.T) {
	broker := helpers.NewRuntimeAuthBroker(t)

	ticketsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"T-1"}],"total":1}`))
	}))
	t.Cleanup(ticketsAPI.Close)

	usersAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"U-1"}],"total":1}`))
	}))
	t.Cleanup(usersAPI.Close)

	dir := t.TempDir()
	configPath := helpers.WriteRuntimeAuthBrokerConfig(t, dir, broker, ticketsAPI.URL, usersAPI.URL)
	server := runtime.NewServer(runtime.Options{
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
	})
	runtimeServer := httptest.NewServer(server.Handler())
	t.Cleanup(runtimeServer.Close)

	fr := helpers.NewFindingsRecorder("remote-runtime-failures")
	fr.SetLaneMetadata("product-validation", "remote-runtime", "ci-containerized", "oauthClient")
	defer fr.MustEmitToTest(t)

	t.Run("missing-bearer-token", func(t *testing.T) {
		resp, body := executeRuntimeRequest(t, http.MethodGet, runtimeServer.URL+"/v1/catalog/effective?config="+url.QueryEscape(configPath), "", nil)
		defer resp.Body.Close()
		fr.Check("missing-token-status", "catalog request without runtime bearer token is rejected", "401", http.StatusText(resp.StatusCode), resp.StatusCode == http.StatusUnauthorized, body)
		fr.Check("missing-token-body", "missing runtime bearer token returns authn_failed", "authn_failed", body, body == "authn_failed", "")
	})

	t.Run("wrong-scope-token", func(t *testing.T) {
		token := broker.AcquireClientCredentialsToken(t, "microsoft", "oclird", []string{"tool:users:listUsers"})
		resp, body := executeRuntimeRequest(t, http.MethodPost, runtimeServer.URL+"/v1/tools/execute", token, map[string]any{
			"configPath": configPath,
			"toolId":     "tickets:listTickets",
		})
		defer resp.Body.Close()
		fr.Check("wrong-scope-status", "token without matching tool scope is rejected", "403", http.StatusText(resp.StatusCode), resp.StatusCode == http.StatusForbidden, body)
		fr.Check("wrong-scope-body", "wrong-scope execution returns authz_denied", "authz_denied", body, body == "authz_denied", "")
	})

	t.Run("expired-token", func(t *testing.T) {
		expiredToken := broker.AcquireExpiredClientCredentialsToken(t, "microsoft", "oclird", []string{"bundle:tickets", "tool:tickets:listTickets"})
		resp, body := executeRuntimeRequest(t, http.MethodGet, runtimeServer.URL+"/v1/catalog/effective?config="+url.QueryEscape(configPath), expiredToken, nil)
		defer resp.Body.Close()
		fr.Check("expired-token-status", "expired runtime bearer token is rejected", "401", http.StatusText(resp.StatusCode), resp.StatusCode == http.StatusUnauthorized, body)
		fr.Check("expired-token-body", "expired runtime bearer token returns authn_failed", "authn_failed", body, body == "authn_failed", "")
	})

	t.Run("invalid-token", func(t *testing.T) {
		token := broker.AcquireClientCredentialsToken(t, "microsoft", "oclird", []string{"bundle:tickets", "tool:tickets:listTickets"})
		invalidToken := token[:len(token)-2] + "zz"
		resp, body := executeRuntimeRequest(t, http.MethodGet, runtimeServer.URL+"/v1/catalog/effective?config="+url.QueryEscape(configPath), invalidToken, nil)
		defer resp.Body.Close()
		fr.Check("invalid-token-status", "runtime rejects a tampered bearer token", "401", http.StatusText(resp.StatusCode), resp.StatusCode == http.StatusUnauthorized, body)
		fr.Check("invalid-token-body", "tampered runtime bearer token returns authn_failed", "authn_failed", body, body == "authn_failed", "")
	})
}
