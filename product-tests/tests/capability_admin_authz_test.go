package tests_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/admin/authn"
	"github.com/StevenBuglione/open-cli/internal/admin/httpapi"
	"github.com/StevenBuglione/open-cli/internal/admin/service"
	"github.com/StevenBuglione/open-cli/internal/admin/store"
	_ "modernc.org/sqlite"
)

// setupAuthzTestAdmin creates an admin server for authorization testing
func setupAuthzTestAdmin(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	st := store.New(db)
	if err := st.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	svc := service.New(st)

	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			switch token {
			case "admin-token":
				return &authn.AdminIdentity{
					Subject: "admin@example.com",
					Name:    "Admin User",
					Groups:  []string{"admins"},
					IsAdmin: true,
				}, nil
			case "user-token":
				return &authn.AdminIdentity{
					Subject: "user@example.com",
					Name:    "Regular User",
					Groups:  []string{"users"},
					IsAdmin: false,
				}, nil
			default:
				return nil, authn.ErrInvalidToken
			}
		},
	}

	deps := httpapi.NewDependencies(svc, verifier)
	router := httpapi.RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)

	cleanup := func() {
		server.Close()
		db.Close()
	}

	return server, cleanup
}

// TestAdminAuthzRequiresToken tests that all admin endpoints require authentication
func TestAdminAuthzRequiresToken(t *testing.T) {
	server, cleanup := setupAuthzTestAdmin(t)
	defer cleanup()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/admin/me"},
		{http.MethodGet, "/v1/admin/bundles"},
		{http.MethodPost, "/v1/admin/bundles"},
		{http.MethodGet, "/v1/admin/sources"},
		{http.MethodPost, "/v1/admin/sources"},
		{http.MethodGet, "/v1/admin/audit/events"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path+"_no_token", func(t *testing.T) {
			req, err := http.NewRequest(ep.method, server.URL+ep.path, nil)
			if err != nil {
				t.Fatal(err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("expected 401 without token, got %d", resp.StatusCode)
			}
		})
	}
}

// TestAdminAuthzRequiresAdminRole tests that non-admin users are denied access
func TestAdminAuthzRequiresAdminRole(t *testing.T) {
	server, cleanup := setupAuthzTestAdmin(t)
	defer cleanup()

	const userToken = "user-token"

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/admin/bundles"},
		{http.MethodPost, "/v1/admin/bundles"},
		{http.MethodGet, "/v1/admin/sources"},
		{http.MethodPost, "/v1/admin/sources"},
		{http.MethodGet, "/v1/admin/audit/events"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path+"_user_denied", func(t *testing.T) {
			req, err := http.NewRequest(ep.method, server.URL+ep.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+userToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("expected 403 for non-admin user, got %d", resp.StatusCode)
			}
		})
	}
}

// TestAdminAuthzAllowsAdminRole tests that admin users can access endpoints
func TestAdminAuthzAllowsAdminRole(t *testing.T) {
	server, cleanup := setupAuthzTestAdmin(t)
	defer cleanup()

	const adminToken = "admin-token"

	endpoints := []struct {
		method         string
		path           string
		expectedStatus int
	}{
		{http.MethodGet, "/v1/admin/me", http.StatusOK},
		{http.MethodGet, "/v1/admin/bundles", http.StatusOK},
		{http.MethodGet, "/v1/admin/sources", http.StatusOK},
		{http.MethodGet, "/v1/admin/audit/events", http.StatusOK},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path+"_admin_allowed", func(t *testing.T) {
			req, err := http.NewRequest(ep.method, server.URL+ep.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+adminToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if resp.StatusCode != ep.expectedStatus {
				t.Errorf("expected %d for admin user, got %d", ep.expectedStatus, resp.StatusCode)
			}
		})
	}
}

// TestAdminAuthzInvalidToken tests that invalid tokens are rejected
func TestAdminAuthzInvalidToken(t *testing.T) {
	server, cleanup := setupAuthzTestAdmin(t)
	defer cleanup()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/admin/bundles", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer invalid-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d", resp.StatusCode)
	}
}

// TestAdminAuthzMalformedAuthHeader tests that malformed auth headers are rejected
func TestAdminAuthzMalformedAuthHeader(t *testing.T) {
	server, cleanup := setupAuthzTestAdmin(t)
	defer cleanup()

	testCases := []struct {
		name   string
		header string
	}{
		{"no_bearer_prefix", "token123"},
		{"wrong_prefix", "Basic token123"},
		{"empty_token", "Bearer "},
		{"just_bearer", "Bearer"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/admin/bundles", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", tc.header)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("expected 401 for malformed header %q, got %d", tc.header, resp.StatusCode)
			}
		})
	}
}
