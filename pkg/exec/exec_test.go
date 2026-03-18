package exec_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	httpexec "github.com/StevenBuglione/open-cli/pkg/exec"
)

func TestExecuteBuildsRequestsAndAppliesAuthAdapters(t *testing.T) {
	cases := []struct {
		name           string
		auth           httpexec.AuthScheme
		expectedHeader string
		expectedQuery  string
	}{
		{
			name:           "bearer",
			auth:           httpexec.AuthScheme{Type: "http", Scheme: "bearer", Value: "token-123"},
			expectedHeader: "Bearer token-123",
		},
		{
			name:           "basic",
			auth:           httpexec.AuthScheme{Type: "http", Scheme: "basic", Value: "aladdin:opensesame"},
			expectedHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte("aladdin:opensesame")),
		},
		{
			name:          "api-key",
			auth:          httpexec.AuthScheme{Type: "apiKey", In: "query", Name: "api_key", Value: "secret"},
			expectedQuery: "api_key=secret&status=open",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/tickets/T-123" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				if got := r.URL.RawQuery; tc.expectedQuery != "" && got != tc.expectedQuery {
					t.Fatalf("unexpected query: %s", got)
				}
				if tc.expectedHeader != "" && r.Header.Get("Authorization") != tc.expectedHeader {
					t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
				}
				if got := r.Header.Get("X-Trace-Id"); got != "trace-1" {
					t.Fatalf("unexpected trace header: %q", got)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body["title"] != "Sample" {
					t.Fatalf("unexpected body: %#v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			}))
			defer server.Close()

			tool := catalog.Tool{
				ID:         "tickets:createTicket",
				Method:     http.MethodPost,
				Path:       "/tickets/{id}",
				PathParams: []catalog.Parameter{{Name: "id", OriginalName: "id", Location: "path", Required: true}},
				Flags: []catalog.Parameter{
					{Name: "state", OriginalName: "status", Location: "query"},
					{Name: "trace-id", OriginalName: "X-Trace-Id", Location: "header"},
				},
				Servers: []string{server.URL},
			}

			result, err := httpexec.Execute(context.Background(), http.DefaultClient, httpexec.Request{
				Tool:     tool,
				PathArgs: []string{"T-123"},
				Flags: map[string]string{
					"state":    "open",
					"trace-id": "trace-1",
				},
				Body: []byte(`{"title":"Sample"}`),
				Auth: []httpexec.AuthScheme{tc.auth},
			})
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if result.StatusCode != http.StatusOK {
				t.Fatalf("unexpected status: %d", result.StatusCode)
			}
			if !json.Valid(result.Body) {
				t.Fatalf("expected json response body, got %q", result.Body)
			}
		})
	}
}
