package tests_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignRemoteAPIConcurrencyIsolation(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/items" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		page := r.URL.Query().Get("page")
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{"id": "item-" + page}},
			"page":  page,
			"total": 1,
		})
	}))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "concurrency.openapi.yaml", gapOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, "concurrency.cli.json", gapCLIConfig(openapiPath))

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	runtimeServer := httptest.NewServer(server.Handler())
	t.Cleanup(runtimeServer.Close)

	fr := helpers.NewFindingsRecorder("remote-api-concurrency-isolation")
	fr.SetLaneMetadata("product-validation", "remote-api", "ci-containerized", "none")
	defer fr.MustEmitToTest(t)

	type callResult struct {
		page       string
		statusCode float64
		bodyPage   string
		err        error
	}
	results := make(chan callResult, 3)
	var wg sync.WaitGroup
	for _, page := range []string{"1", "2", "3"} {
		wg.Add(1)
		go func(page string) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]any{
				"configPath": configPath,
				"toolId":     "gap:listItems",
				"flags":      map[string]string{"page": page},
			})
			resp, err := http.Post(runtimeServer.URL+"/v1/tools/execute", "application/json", bytes.NewReader(payload))
			if err != nil {
				results <- callResult{page: page, err: err}
				return
			}
			defer resp.Body.Close()
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				results <- callResult{page: page, err: err}
				return
			}
			statusCode, _ := body["statusCode"].(float64)
			responseBody, _ := body["body"].(map[string]any)
			results <- callResult{
				page:       page,
				statusCode: statusCode,
				bodyPage:   fmt.Sprintf("%v", responseBody["page"]),
			}
		}(page)
	}
	wg.Wait()
	close(results)

	matches := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent execute for page %s: %v", result.page, result.err)
		}
		if result.statusCode == 200 && result.bodyPage == result.page {
			matches++
		}
	}
	fr.Check("concurrency-match-count", "concurrent remote API calls return isolated results for each request", "3", fmt.Sprintf("%d", matches), matches == 3, "")

	inst := &helpers.Instance{URL: runtimeServer.URL, AuditPath: filepath.Join(dir, "audit.log")}
	events := inst.AuditEvents(t)
	fr.Check("concurrency-audit-events", "audit trail records every concurrent request", "3", fmt.Sprintf("%d", len(events)), len(events) == 3, "")
}
