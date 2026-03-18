package tests_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignRemoteAPINonJSONResponse(t *testing.T) {
	textAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plaintext" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("plain failure body"))
	}))
	t.Cleanup(textAPI.Close)

	dir := t.TempDir()
	textSpec := writeFile(t, dir, "plaintext.openapi.yaml", `openapi: 3.1.0
info:
  title: Plaintext API
  version: "1.0.0"
servers:
  - url: `+textAPI.URL+`
paths:
  /plaintext:
    get:
      operationId: getPlaintext
      tags: [plaintext]
      responses:
        "502":
          description: Bad Gateway
`)
	textConfig := writeFile(t, dir, "plaintext.cli.json", restCLIConfig(textSpec))

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	runtimeServer := httptest.NewServer(server.Handler())
	t.Cleanup(runtimeServer.Close)

	fr := helpers.NewFindingsRecorder("remote-api-nonjson-response")
	fr.SetLaneMetadata("product-validation", "remote-api", "ci-containerized", "none")
	defer fr.MustEmitToTest(t)

	result := executeTool(t, runtimeServer.URL, textConfig, "testapi:getPlaintext", nil)
	statusCode, _ := result["statusCode"].(float64)
	fr.Check("nonjson-status", "non-JSON upstream status is surfaced", "502", fmt.Sprintf("%.0f", statusCode), statusCode == 502, "")
	text, _ := result["text"].(string)
	fr.Check("nonjson-text", "non-JSON upstream body is preserved in the text field", "plain failure body", strings.TrimSpace(text), strings.TrimSpace(text) == "plain failure body", "")
	contentType, _ := result["contentType"].(string)
	fr.Check("nonjson-content-type", "non-JSON upstream responses expose the upstream content type", "text/plain; charset=utf-8", contentType, contentType == "text/plain; charset=utf-8", "")
}
