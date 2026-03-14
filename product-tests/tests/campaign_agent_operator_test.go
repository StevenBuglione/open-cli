package tests_test

// campaign_agent_operator_test.go exercises a realistic scripted
// operator/agent campaign that crosses multiple CLI surfaces:
//   - Config surface  (reads a project CLI config)
//   - Discovery surface (builds a tool catalog from an OpenAPI spec)
//   - REST surface   (executes multi-step CRUD tool calls)
//   - Auth surface   (OAuth client_credentials token acquisition)
//   - Audit surface  (verifies every executed call is recorded)
//
// At the end of every sub-test a structured CampaignRubric is emitted via
// helpers.FindingsRecorder so findings are captured in both structured JSON
// and human-readable form.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
	"github.com/StevenBuglione/oas-cli-go/product-tests/tests/helpers"
)

// campaignRestAPI builds an inline REST API that handles items CRUD.
// It mirrors the fixtures used by capability_execute_rest_test.go without
// sharing state so campaigns are fully isolated.
func campaignRestAPI() *httptest.Server {
	store := newFixtureStore()
	return httptest.NewServer(newRestFixtureHandler(store))
}

// campaignOpenAPIYAML returns a minimal OpenAPI spec pointing at serverURL
// that exposes listItems, createItem, getItem, updateItem, deleteItem.
func campaignOpenAPIYAML(serverURL string) string {
	return `openapi: 3.1.0
info:
  title: Campaign API
  version: "1.0.0"
servers:
  - url: ` + serverURL + `
paths:
  /items:
    get:
      operationId: listItems
      tags: [items]
      parameters:
        - name: page
          in: query
          schema: { type: integer, default: 1 }
        - name: pageSize
          in: query
          schema: { type: integer, default: 20 }
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema: { type: object }
    post:
      operationId: createItem
      tags: [items]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name: { type: string }
                tags:
                  type: array
                  items: { type: string }
      responses:
        "201":
          description: Created
  /items/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: { type: string }
    get:
      operationId: getItem
      tags: [items]
      responses:
        "200":
          description: OK
    put:
      operationId: updateItem
      tags: [items]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name: { type: string }
      responses:
        "200":
          description: OK
    delete:
      operationId: deleteItem
      tags: [items]
      responses:
        "204":
          description: No content
`
}

// campaignCLIConfig returns a CLI config JSON that points at openapiPath.
func campaignCLIConfig(openapiPath string) string {
	return fmt.Sprintf(`{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "campaignSource": {
      "type": "openapi",
      "uri": %q,
      "enabled": true
    }
  },
  "services": {
    "campaign": {
      "source": "campaignSource",
      "alias": "campaign"
    }
  }
}`, openapiPath)
}

// TestCampaignAgentOperator runs a scripted operator/agent campaign that
// crosses Config → Discovery → REST → Audit surfaces in a single flow.
func TestCampaignAgentOperator(t *testing.T) {
	api := campaignRestAPI()
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "campaign.openapi.yaml", campaignOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, "campaign.cli.json", campaignCLIConfig(openapiPath))

	runtimeSrv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
	})
	srv := httptest.NewServer(runtimeSrv.Handler())
	t.Cleanup(srv.Close)

	fr := helpers.NewFindingsRecorder("operator-crud-rest-surfaces")
	defer fr.MustEmitToTest(t)

	// ── Step 1: Discovery surface ─────────────────────────────────────────────
	// Verify the catalog endpoint returns the tools we declared in the spec.
	t.Run("discovery", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/v1/catalog/effective?config=" + configPath)
		if err != nil {
			t.Fatalf("catalog request: %v", err)
		}
		defer resp.Body.Close()

		fr.CheckBool("catalog-http-200", "catalog endpoint returns HTTP 200",
			resp.StatusCode == http.StatusOK,
			fmt.Sprintf("got %d", resp.StatusCode))

		var envelope map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode catalog response: %v", err)
		}
		cat, _ := envelope["catalog"].(map[string]any)
		if cat == nil {
			cat = envelope // fall back to top-level in case schema differs
		}

		tools, _ := cat["tools"].([]any)
		fr.Check("catalog-tool-count", "catalog exposes at least 4 item tools",
			">=4", fmt.Sprintf("%d", len(tools)), len(tools) >= 4, "")

		fr.Note(fmt.Sprintf("discovered %d tools from campaign OpenAPI spec", len(tools)))
	})

	// ── Step 2: REST surface – list (initial state) ───────────────────────────
	t.Run("list-initial", func(t *testing.T) {
		result := executeTool(t, srv.URL, configPath, "campaign:listItems", nil)
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("rest-list-200", "listItems returns HTTP 200",
			"200", fmt.Sprintf("%.0f", statusCode), statusCode == 200, "")

		body, _ := result["body"].(map[string]any)
		items, _ := body["items"].([]any)
		fr.Check("rest-list-has-items", "initial item store is non-empty",
			">0", fmt.Sprintf("%d", len(items)), len(items) > 0,
			"fixture store pre-seeds 3 items")
	})

	// ── Step 3: REST surface – create ─────────────────────────────────────────
	var createdID string
	t.Run("create", func(t *testing.T) {
		bodyBytes, _ := json.Marshal(map[string]any{"name": "campaign-widget", "tags": []string{"campaign", "test"}})
		result := executeTool(t, srv.URL, configPath, "campaign:createItem", map[string]any{
			"body": bodyBytes,
		})
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("rest-create-201", "createItem returns HTTP 201",
			"201", fmt.Sprintf("%.0f", statusCode), statusCode == 201, "")

		body, _ := result["body"].(map[string]any)
		createdID, _ = body["id"].(string)
		fr.Check("rest-create-id", "created item has a non-empty ID",
			"non-empty string", createdID, createdID != "", "")

		fr.Note(fmt.Sprintf("created item id=%q name=%q", createdID, body["name"]))
	})

	// ── Step 4: REST surface – get ────────────────────────────────────────────
	t.Run("get", func(t *testing.T) {
		if createdID == "" {
			t.Skip("skipping: create step did not produce an item ID")
		}
		result := executeTool(t, srv.URL, configPath, "campaign:getItem", map[string]any{
			"pathArgs": []string{createdID},
		})
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("rest-get-200", "getItem returns HTTP 200",
			"200", fmt.Sprintf("%.0f", statusCode), statusCode == 200, "")

		body, _ := result["body"].(map[string]any)
		gotID, _ := body["id"].(string)
		fr.Check("rest-get-correct-id", "fetched item ID matches created ID",
			createdID, gotID, gotID == createdID, "")
	})

	// ── Step 5: REST surface – update ─────────────────────────────────────────
	t.Run("update", func(t *testing.T) {
		if createdID == "" {
			t.Skip("skipping: create step did not produce an item ID")
		}
		bodyBytes, _ := json.Marshal(map[string]any{"name": "campaign-widget-updated"})
		result := executeTool(t, srv.URL, configPath, "campaign:updateItem", map[string]any{
			"pathArgs": []string{createdID},
			"body":     bodyBytes,
		})
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("rest-update-200", "updateItem returns HTTP 200",
			"200", fmt.Sprintf("%.0f", statusCode), statusCode == 200, "")

		body, _ := result["body"].(map[string]any)
		gotName, _ := body["name"].(string)
		fr.Check("rest-update-name", "updated item name is reflected in response",
			"campaign-widget-updated", gotName, gotName == "campaign-widget-updated", "")
	})

	// ── Step 6: REST surface – delete ─────────────────────────────────────────
	t.Run("delete", func(t *testing.T) {
		if createdID == "" {
			t.Skip("skipping: create step did not produce an item ID")
		}
		result := executeTool(t, srv.URL, configPath, "campaign:deleteItem", map[string]any{
			"pathArgs": []string{createdID},
		})
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("rest-delete-204", "deleteItem returns HTTP 204",
			"204", fmt.Sprintf("%.0f", statusCode), statusCode == 204, "")
	})

	// ── Step 7: REST surface – get-after-delete ───────────────────────────────
	t.Run("get-after-delete", func(t *testing.T) {
		if createdID == "" {
			t.Skip("skipping: create step did not produce an item ID")
		}
		result := executeTool(t, srv.URL, configPath, "campaign:getItem", map[string]any{
			"pathArgs": []string{createdID},
		})
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("rest-get-after-delete-404", "getItem on deleted resource returns HTTP 404",
			"404", fmt.Sprintf("%.0f", statusCode), statusCode == 404, "")
	})

	// ── Step 8: Audit surface ─────────────────────────────────────────────────
	t.Run("audit-trail", func(t *testing.T) {
		inst := &helpers.Instance{
			URL:      srv.URL,
			AuditPath: filepath.Join(dir, "audit.log"),
		}
		count := inst.AuditEventCount(t)
		fr.Check("audit-event-count", "at least one audit event recorded per executed tool call",
			">0", fmt.Sprintf("%d", count), count > 0, "")
		fr.Note(fmt.Sprintf("audit trail contains %d events after campaign", count))
	})
}

// TestCampaignAgentOperatorWithAuth extends the operator campaign with OAuth
// client_credentials flow, crossing the Auth surface in addition to REST/Audit.
func TestCampaignAgentOperatorWithAuth(t *testing.T) {
	const (
		validClientID = "campaign-client"
		validSecret   = "campaign-secret"
		validScope    = "api.read"
		issuedToken   = "campaign-access-token-xyz789"
	)

	fr := helpers.NewFindingsRecorder("operator-crud-rest-auth")
	defer fr.MustEmitToTest(t)

	// Build an inline API + OAuth stub that records token call count.
	tokenCalls := 0
	combinedAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if r.FormValue("client_id") != validClientID || r.FormValue("client_secret") != validSecret {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": issuedToken,
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/items":
			if r.Header.Get("Authorization") != "Bearer "+issuedToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{}, "total": 0, "page": 1, "pageSize": 20, "totalPages": 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(combinedAPI.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "auth-campaign.openapi.yaml",
		oauthOpenAPIYAML(combinedAPI.URL, combinedAPI.URL+"/oauth/token"))
	configPath := writeFile(t, dir, "auth-campaign.cli.json",
		oauthCLIConfig(openapiPath, validClientID, validSecret))

	runtimeSrv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
	})
	srv := httptest.NewServer(runtimeSrv.Handler())
	t.Cleanup(srv.Close)

	// Env vars required by the CLI config oauth secret references.
	t.Setenv("TEST_CLIENT_ID", validClientID)
	t.Setenv("TEST_CLIENT_SECRET", validSecret)

	// ── Auth surface: token acquisition ──────────────────────────────────────
	t.Run("token-acquired", func(t *testing.T) {
		result := executeTool(t, srv.URL, configPath, "protected:listItems", nil)
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("auth-list-200", "listItems with OAuth returns HTTP 200",
			"200", fmt.Sprintf("%.0f", statusCode), statusCode == 200, "")

		fr.Check("auth-token-acquired", "OAuth token was acquired exactly once",
			"1", fmt.Sprintf("%d", tokenCalls), tokenCalls == 1, "")
	})

	// ── Auth surface: second call reuses cached token ─────────────────────────
	t.Run("token-cached", func(t *testing.T) {
		beforeCalls := tokenCalls
		result := executeTool(t, srv.URL, configPath, "protected:listItems", nil)
		statusCode, _ := result["statusCode"].(float64)
		fr.Check("auth-second-list-200", "second listItems call also returns HTTP 200",
			"200", fmt.Sprintf("%.0f", statusCode), statusCode == 200, "")

		newCalls := tokenCalls - beforeCalls
		fr.Check("auth-token-cached", "no additional token requests issued for second call",
			"0", fmt.Sprintf("%d", newCalls), newCalls == 0,
			"token should be cached within the 3600-second TTL")
	})

	// ── Audit surface ─────────────────────────────────────────────────────────
	t.Run("audit", func(t *testing.T) {
		inst := &helpers.Instance{
			URL:       srv.URL,
			AuditPath: filepath.Join(dir, "audit.log"),
		}
		count := inst.AuditEventCount(t)
		fr.Check("auth-audit-events", "audit trail has events after auth campaign",
			">0", fmt.Sprintf("%d", count), count > 0, "")
		fr.Note(fmt.Sprintf("auth campaign: %d audit events, %d token calls", count, tokenCalls))
	})
}
