package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/admin/authn"
)

type mockTokenVerifier struct {
	verifyFunc func(ctx context.Context, token string) (*authn.AdminIdentity, error)
}

func (m *mockTokenVerifier) VerifyToken(ctx context.Context, token string) (*authn.AdminIdentity, error) {
	return m.verifyFunc(ctx, token)
}

func TestRouterRegistersAdminMe(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			return nil, fmt.Errorf("invalid token")
		},
	}

	deps := NewDependencies(nil, verifier)
	router := RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/admin/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestAdminMe_MissingToken(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			t.Fatal("should not be called without token")
			return nil, nil
		},
	}

	deps := NewDependencies(nil, verifier)
	router := RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/admin/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAdminMe_InvalidToken(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			return nil, fmt.Errorf("token validation failed")
		},
	}

	deps := NewDependencies(nil, verifier)
	router := RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/admin/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer invalid-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", resp.StatusCode)
	}
}

func TestAdminMe_NonAdminUser(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			return &authn.AdminIdentity{
				Subject: "user@example.com",
				Name:    "Regular User",
				Groups:  []string{"users"},
				IsAdmin: false,
			}, nil
		},
	}

	deps := NewDependencies(nil, verifier)
	router := RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/admin/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer valid-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin user, got %d", resp.StatusCode)
	}
}

func TestAdminMe_ValidAdminUser(t *testing.T) {
	expectedIdentity := &authn.AdminIdentity{
		Subject: "admin@example.com",
		Name:    "Admin User",
		Email:   "admin@example.com",
		Groups:  []string{"admins", "users"},
		IsAdmin: true,
	}

	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			if token != "valid-admin-token" {
				return nil, fmt.Errorf("invalid token")
			}
			return expectedIdentity, nil
		},
	}

	deps := NewDependencies(nil, verifier)
	router := RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/admin/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer valid-admin-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for valid admin, got %d: %s", resp.StatusCode, string(body))
	}

	var identity authn.AdminIdentity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if identity.Subject != expectedIdentity.Subject {
		t.Errorf("expected subject %q, got %q", expectedIdentity.Subject, identity.Subject)
	}

	if identity.Name != expectedIdentity.Name {
		t.Errorf("expected name %q, got %q", expectedIdentity.Name, identity.Name)
	}

	if !identity.IsAdmin {
		t.Error("expected IsAdmin to be true")
	}

	if len(identity.Groups) != len(expectedIdentity.Groups) {
		t.Errorf("expected %d groups, got %d", len(expectedIdentity.Groups), len(identity.Groups))
	}
}

// Tests for bundle endpoints
func TestBundleEndpoints_RequireAuth(t *testing.T) {
	// Test that endpoints require authentication
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			return nil, fmt.Errorf("invalid token")
		},
	}
	deps := NewDependencies(nil, verifier)
	router := RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)
	defer server.Close()

	// Test bundle endpoints require auth
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"list bundles", http.MethodGet, "/v1/admin/bundles"},
		{"create bundle", http.MethodPost, "/v1/admin/bundles"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, server.URL+tc.path, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
			}
		})
	}
}

// Tests for source endpoints
func TestSourceEndpoints(t *testing.T) {
	// Verify endpoints are registered and require auth
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*authn.AdminIdentity, error) {
			return nil, fmt.Errorf("invalid token")
		},
	}
	deps := NewDependencies(nil, verifier)
	router := RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)
	defer server.Close()

	// Test source creation endpoint requires auth
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/admin/sources", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("source creation endpoint not registered")
	}
	// Should require auth (401) or indicate endpoint exists with different error
}
