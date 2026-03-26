package tests_test

// campaign_known_gaps_test.go documents expected-failure scenarios and known
// feature gaps in the ocli product.  These tests run during every campaign
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

	"github.com/StevenBuglione/open-cli/internal/runtime"
	"github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
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

}

// TestCampaignKnownGapsAuth documents known gaps in the authentication surface.
func TestCampaignKnownGapsAuth(t *testing.T) {
	fr := helpers.NewFindingsRecorder("known-gaps-auth")
	defer fr.MustEmitToTest(t)

	// ── Gap 5: Device-code / interactive OAuth flows ──────────────────────────
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

	t.Run("gap-token-revocation", func(t *testing.T) {
		fr.RecordKnownGap(
			"gap-token-revocation",
			"Runtime bearer tokens can be rejected after revocation, not only by expiry/signature validation",
			"Runtime auth currently validates JWT structure, signature, issuer, audience, and scopes, but does not perform revocation or introspection checks",
			func() bool {
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
  "runtime": {
    "mode": "remote"
  },
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
