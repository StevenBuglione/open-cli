package tests_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignRemoteAPIPaginationForwarding(t *testing.T) {
	var pageSeen string
	var pageSizeSeen string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/items" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		pageSeen = r.URL.Query().Get("page")
		pageSizeSeen = r.URL.Query().Get("pageSize")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":      []any{},
			"page":       pageSeen,
			"pageSize":   pageSizeSeen,
			"total":      0,
			"totalPages": 0,
		})
	}))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "pagination.openapi.yaml", gapOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, "pagination.cli.json", gapCLIConfig(openapiPath))

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	runtimeServer := httptest.NewServer(server.Handler())
	t.Cleanup(runtimeServer.Close)

	fr := helpers.NewFindingsRecorder("remote-api-pagination-forwarding")
	fr.SetLaneMetadata("product-validation", "remote-api", "ci-containerized", "none")
	defer fr.MustEmitToTest(t)

	result := executeTool(t, runtimeServer.URL, configPath, "gap:listItems", map[string]any{
		"flags": map[string]string{"page": "2", "page-size": "5"},
	})
	statusCode, _ := result["statusCode"].(float64)
	fr.Check("pagination-status", "paginated remote API request succeeds", "200", fmt.Sprintf("%.0f", statusCode), statusCode == 200, "")
	fr.Check("pagination-page-forwarded", "page flag is forwarded to the upstream API", "2", pageSeen, pageSeen == "2", "")
	fr.Check("pagination-page-size-forwarded", "pageSize flag is forwarded to the upstream API", "5", pageSizeSeen, pageSizeSeen == "5", "")
	body, _ := result["body"].(map[string]any)
	fr.Check("pagination-body-page", "runtime response preserves the upstream page value", "2", fmt.Sprintf("%v", body["page"]), fmt.Sprintf("%v", body["page"]) == "2", "")
	fr.Check("pagination-body-page-size", "runtime response preserves the upstream pageSize value", "5", fmt.Sprintf("%v", body["pageSize"]), fmt.Sprintf("%v", body["pageSize"]) == "5", "")
}
