package tests_test

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/runtime"
)

// ---- REST capability fixtures ----

func restOpenAPIYAML(serverURL string) string {
	return `openapi: 3.1.0
info:
  title: Test API
  version: "1.0.0"
servers:
  - url: ` + serverURL + `
paths:
  /items:
    get:
      operationId: listItems
      tags: [items]
      parameters:
        - name: tag
          in: query
          schema: { type: string }
        - name: page
          in: query
          schema: { type: integer }
        - name: pageSize
          in: query
          schema: { type: integer }
      responses:
        "200":
          description: OK
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
          description: Deleted
  /errors/unauthorized:
    get:
      operationId: triggerUnauthorized
      tags: [errors]
      responses:
        "401":
          description: Unauthorized
  /errors/forbidden:
    get:
      operationId: triggerForbidden
      tags: [errors]
      responses:
        "403":
          description: Forbidden
  /errors/rate-limited:
    get:
      operationId: triggerRateLimited
      tags: [errors]
      responses:
        "429":
          description: Too Many Requests
  /errors/internal:
    get:
      operationId: triggerInternalError
      tags: [errors]
      responses:
        "500":
          description: Internal Error
  /operations:
    post:
      operationId: createOperation
      tags: [operations]
      responses:
        "202":
          description: Accepted
  /operations/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: { type: string }
    get:
      operationId: getOperation
      tags: [operations]
      responses:
        "200":
          description: OK
`
}

func restCLIConfig(openapiPath string) string {
	return `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "testapiSource": {
      "type": "openapi",
      "uri": "` + openapiPath + `",
      "enabled": true
    }
  },
  "services": {
    "testapi": {
      "source": "testapiSource",
      "alias": "testapi"
    }
  }
}`
}

// ---- tests ----

func TestCapabilityExecuteREST(t *testing.T) {
	store := newFixtureStore()
	api := httptest.NewServer(newRestFixtureHandler(store))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "testapi.openapi.yaml", restOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, ".cli.json", restCLIConfig(openapiPath))

	srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	runtimeSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(runtimeSrv.Close)

	t.Run("ListItems", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:listItems", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200, got %v", result)
		}
	})

	t.Run("GetItem", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:getItem",
			map[string]any{"pathArgs": []string{"item-1"}})
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200, got %v", result)
		}
	})

	t.Run("GetItemNotFound", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:getItem",
			map[string]any{"pathArgs": []string{"does-not-exist"}})
		if got, ok := result["statusCode"].(float64); !ok || got != 404 {
			t.Fatalf("expected statusCode 404, got %v", result)
		}
	})

	t.Run("CreateItem", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "New Item", "tags": []string{"test"}})
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:createItem",
			map[string]any{"body": body})
		if got, ok := result["statusCode"].(float64); !ok || got != 201 {
			t.Fatalf("expected statusCode 201, got %v", result)
		}
	})

	t.Run("UpdateItem", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Updated Item"})
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:updateItem",
			map[string]any{"pathArgs": []string{"item-2"}, "body": body})
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200, got %v", result)
		}
	})

	t.Run("DeleteItem", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:deleteItem",
			map[string]any{"pathArgs": []string{"item-3"}})
		if got, ok := result["statusCode"].(float64); !ok || got != 204 {
			t.Fatalf("expected statusCode 204, got %v", result)
		}
	})

	t.Run("PaginationViaFlags", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:listItems",
			map[string]any{"flags": map[string]string{"page": "1", "pageSize": "2"}})
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200 for pagination, got %v", result)
		}
	})

	t.Run("FilterByTag", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:listItems",
			map[string]any{"flags": map[string]string{"tag": "missing-tag"}})
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200 for tag filter, got %v", result)
		}
	})

	t.Run("ErrorUnauthorized", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:triggerUnauthorized", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 401 {
			t.Fatalf("expected statusCode 401, got %v", result)
		}
	})

	t.Run("ErrorForbidden", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:triggerForbidden", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 403 {
			t.Fatalf("expected statusCode 403, got %v", result)
		}
	})

	t.Run("ErrorRateLimited", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:triggerRateLimited", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 429 {
			t.Fatalf("expected statusCode 429, got %v", result)
		}
	})

	t.Run("ErrorInternalServer", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:triggerInternalError", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 500 {
			t.Fatalf("expected statusCode 500, got %v", result)
		}
	})

	t.Run("CreateOperation", func(t *testing.T) {
		result := executeTool(t, runtimeSrv.URL, configPath, "testapi:createOperation", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 202 {
			t.Fatalf("expected statusCode 202, got %v", result)
		}
		// Extract operation ID from body and poll it
		var body map[string]any
		if rawBody, ok := result["body"]; ok {
			bodyBytes, _ := json.Marshal(rawBody)
			_ = json.Unmarshal(bodyBytes, &body)
		}
		opID, _ := body["id"].(string)
		if opID == "" {
			t.Fatal("expected operation id in response body")
		}

		pollResult := executeTool(t, runtimeSrv.URL, configPath, "testapi:getOperation",
			map[string]any{"pathArgs": []string{opID}})
		if got, ok := pollResult["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200 for poll, got %v", pollResult)
		}
	})
}
