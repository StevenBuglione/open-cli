package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterRegistersAdminMe(t *testing.T) {
	router := RegisterRoutes(http.NewServeMux(), NewDependencies())
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/admin/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 before auth is implemented, got %d", resp.StatusCode)
	}
}
