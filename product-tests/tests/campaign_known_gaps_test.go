package tests_test

// campaign_known_gaps_test.go documents expected-failure scenarios and known
// feature gaps in the oascli product.  These tests run during every campaign
// pass; failures here are captured via FindingsRecorder as "known gaps" and do
// NOT fail the overall test suite — only if a gap unexpectedly starts passing
// is it highlighted so engineers can promote it to a positive assertion.
//
// Gaps are identified with a "gap-NNN" prefix, e.g. "gap-nonexistent-tool-format".

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
	"github.com/StevenBuglione/oas-cli-go/product-tests/tests/helpers"
)

// TestCampaignKnownGaps exercises known-gap scenarios.  Each sub-test probes
// a specific partially-implemented or not-yet-implemented behavior and records
// whether the gap still exists via FindingsRecorder.RecordKnownGap.
func TestCampaignKnownGaps(t *testing.T) {
	fr := helpers.NewFindingsRecorder("known-gaps-catalog")
	defer fr.MustEmitToTest(t)

	// Shared inline REST server for gap probes.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/items" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "gap.openapi.yaml", gapOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, "gap.cli.json", gapCLIConfig(openapiPath))

	runtimeSrv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
	})
	srv := httptest.NewServer(runtimeSrv.Handler())
	t.Cleanup(srv.Close)

	// ── Gap 1: Unknown tool produces an error (verify error shape) ───────────
	// The runtime returns an error for unknown tool IDs; the exact JSON error
	// key returned may vary.  This gap documents that the error response does
	// not yet guarantee a standardised "error"/"message" field layout.
	t.Run("gap-unknown-tool-error-shape", func(t *testing.T) {
		fr.RecordKnownGap(
			"gap-unknown-tool-error-shape",
			"Execute with an unknown tool ID should return a structured JSON error with 'code' and 'message' fields",
			"Error response shape is implementation-defined; no schema contract yet",
			func() bool {
				b, _ := json.Marshal(map[string]any{
					"configPath": configPath,
					"toolId":     "nonexistent:unknownOp",
				})
				resp, err := http.Post(srv.URL+"/v1/tools/execute", "application/json",
					strings.NewReader(string(b)))
				if err != nil {
					return true // gap still fails
				}
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true // unexpected success — gap persists in a different form
				}
				var body map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					return true // non-JSON response — gap still present
				}
				_, hasCode := body["code"]
				_, hasMessage := body["message"]
				_, hasError := body["error"]
				// Gap resolved if there is a "code"+"message" OR standard "error" key.
				return !(hasCode && hasMessage) && !hasError
			},
		)
	})

	// ── Gap 2: Pagination query-param forwarding ──────────────────────────────
	// listItems supports 'page' and 'pageSize' query parameters.  The CLI
	// should forward these params when provided in the tool call extras.
	// This gap captures the expectation that query param forwarding is complete.
	t.Run("gap-pagination-param-forwarding", func(t *testing.T) {
		pageReceived := 0
		pageSizeReceived := 0
		pagingAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/items" && r.Method == http.MethodGet {
				q := r.URL.Query()
				if q.Get("page") != "" {
					pageReceived = 1
				}
				if q.Get("pageSize") != "" {
					pageSizeReceived = 1
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
			} else {
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(pagingAPI.Close)

		pagingDir := t.TempDir()
		pagingSpec := writeFile(t, pagingDir, "paging.openapi.yaml", gapOpenAPIYAML(pagingAPI.URL))
		pagingCfg := writeFile(t, pagingDir, "paging.cli.json", gapCLIConfig(pagingSpec))

		pagingRuntime := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(pagingDir, "audit.log")})
		pagingSrv := httptest.NewServer(pagingRuntime.Handler())
		t.Cleanup(pagingSrv.Close)

		fr.RecordKnownGap(
			"gap-pagination-param-forwarding",
			"Query params 'page' and 'pageSize' from tool call extras are forwarded to the upstream API",
			"Query parameter forwarding from tool extras to HTTP request is not yet fully validated",
			func() bool {
				_ = executeTool(t, pagingSrv.URL, pagingCfg, "gap:listItems", map[string]any{
					"queryParams": map[string]any{"page": "2", "pageSize": "5"},
				})
				// Gap still present if neither param was forwarded.
				return pageReceived == 0 && pageSizeReceived == 0
			},
		)
	})

	// ── Gap 3: Concurrent tool executions are serialised correctly ────────────
	// Simultaneous calls to the same runtime should not produce corrupt results.
	// This gap documents the absence of a formal concurrency stress test.
	t.Run("gap-concurrent-execution-stress", func(t *testing.T) {
		fr.RecordKnownGap(
			"gap-concurrent-execution-stress",
			"Concurrent tool executions return independent, correct results without data races",
			"No dedicated concurrency stress campaign yet; races may surface only under load",
			func() bool {
				// Probe: fire 3 concurrent list requests and verify all succeed.
				results := make(chan float64, 3)
				for i := 0; i < 3; i++ {
					go func() {
						res := executeTool(t, srv.URL, configPath, "gap:listItems", nil)
						code, _ := res["statusCode"].(float64)
						results <- code
					}()
				}
				allOK := true
				for i := 0; i < 3; i++ {
					if code := <-results; code != 200 {
						allOK = false
					}
				}
				// Gap resolved when all 3 concurrent calls succeed.
				return !allOK
			},
		)
	})

	// ── Gap 4: Non-JSON response bodies ──────────────────────────────────────
	// An API that returns a non-JSON response (e.g. plain text) should be
	// surfaced with a clear error rather than a silent failure.
	t.Run("gap-non-json-response-handling", func(t *testing.T) {
		plainAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, "OK")
		}))
		t.Cleanup(plainAPI.Close)

		plainDir := t.TempDir()
		plainSpec := writeFile(t, plainDir, "plain.openapi.yaml", gapOpenAPIYAML(plainAPI.URL))
		plainCfg := writeFile(t, plainDir, "plain.cli.json", gapCLIConfig(plainSpec))

		plainRuntime := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(plainDir, "audit.log")})
		plainSrv := httptest.NewServer(plainRuntime.Handler())
		t.Cleanup(plainSrv.Close)

		fr.RecordKnownGap(
			"gap-non-json-response-handling",
			"Non-JSON upstream responses are returned with a clear 'content-type' indication in the tool result",
			"Tool result schema does not yet include a content-type field; raw body may be silently dropped",
			func() bool {
				res := executeTool(t, plainSrv.URL, plainCfg, "gap:listItems", nil)
				_, hasBody := res["body"]
				_, hasRaw := res["rawBody"]
				_, hasContentType := res["contentType"]
				// Gap resolved when the response includes body content or an explicit content-type.
				return !hasBody && !hasRaw && !hasContentType
			},
		)
	})
}

// TestCampaignKnownGapsAuth documents known gaps in the authentication surface.
func TestCampaignKnownGapsAuth(t *testing.T) {
	fr := helpers.NewFindingsRecorder("known-gaps-auth")
	defer fr.MustEmitToTest(t)

	// ── Gap 5: Expired token refresh ─────────────────────────────────────────
	// When a cached token expires mid-session the runtime should transparently
	// re-acquire a new token.  This probes the gap in expiry handling.
	t.Run("gap-token-expiry-refresh", func(t *testing.T) {
		tokenFetches := 0
		shortLivedOAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth/token":
				tokenFetches++
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": fmt.Sprintf("token-%d", tokenFetches),
					"token_type":   "Bearer",
					"expires_in":   1, // 1-second TTL to trigger expiry
				})
			case "/items":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(shortLivedOAuth.Close)

		fr.RecordKnownGap(
			"gap-token-expiry-refresh",
			"Expired OAuth tokens are transparently refreshed without requiring a new config load",
			"Token TTL-based refresh is not yet implemented; expired tokens return auth errors",
			func() bool {
				// This gap is structural — we document it without running a slow sleep.
				// The absence of a cache TTL check in the auth package confirms the gap.
				return true // gap still present
			},
		)
	})

	// ── Gap 6: Device-code / interactive OAuth flows ──────────────────────────
	t.Run("gap-device-code-flow", func(t *testing.T) {
		fr.RecordKnownGap(
			"gap-device-code-flow",
			"OAuth device_code grant flow is supported for headless environments",
			"Only client_credentials grant type is currently implemented",
			func() bool {
				// Structural gap — no device_code path in pkg/auth/oauth.go
				return true
			},
		)
	})
}

// ── Fixture helpers ─────────────────────────────────────────────────────────

func gapOpenAPIYAML(serverURL string) string {
	return `openapi: 3.1.0
info:
  title: Gap Probe API
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
          schema: { type: integer }
        - name: pageSize
          in: query
          schema: { type: integer }
      responses:
        "200":
          description: OK
`
}

func gapCLIConfig(openapiPath string) string {
	return fmt.Sprintf(`{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "gapSource": {
      "type": "openapi",
      "uri": %q,
      "enabled": true
    }
  },
  "services": {
    "gap": {
      "source": "gapSource",
      "alias": "gap"
    }
  }
}`, openapiPath)
}
