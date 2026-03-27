package authn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockTokenVerifier struct {
	verifyFunc func(ctx context.Context, token string) (*AdminIdentity, error)
}

func (m *mockTokenVerifier) VerifyToken(ctx context.Context, token string) (*AdminIdentity, error) {
	return m.verifyFunc(ctx, token)
}

func TestMiddleware_MissingToken(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*AdminIdentity, error) {
			return nil, fmt.Errorf("should not be called")
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without token")
	})

	mw := NewMiddleware(verifier)
	wrapped := mw.RequireAdmin(handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_InvalidTokenFormat(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*AdminIdentity, error) {
			return nil, fmt.Errorf("should not be called")
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with invalid token")
	})

	mw := NewMiddleware(verifier)
	wrapped := mw.RequireAdmin(handler)

	testCases := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "sometoken"},
		{"empty bearer", "Bearer "},
		{"only bearer", "Bearer"},
		{"wrong case", "bearer token123"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
			req.Header.Set("Authorization", tc.header)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for %q, got %d", tc.header, rec.Code)
			}
		})
	}
}

func TestMiddleware_TokenVerificationFails(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*AdminIdentity, error) {
			return nil, fmt.Errorf("invalid token")
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when verification fails")
	})

	mw := NewMiddleware(verifier)
	wrapped := mw.RequireAdmin(handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
	req.Header.Set("Authorization", "Bearer valid-format-token")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when verification fails, got %d", rec.Code)
	}
}

func TestMiddleware_NotAdminUser(t *testing.T) {
	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*AdminIdentity, error) {
			return &AdminIdentity{
				Subject: "user@example.com",
				Name:    "Regular User",
				Groups:  []string{"users"},
				IsAdmin: false,
			}, nil
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for non-admin user")
	})

	mw := NewMiddleware(verifier)
	wrapped := mw.RequireAdmin(handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin user, got %d", rec.Code)
	}
}

func TestMiddleware_ValidAdminToken(t *testing.T) {
	expectedIdentity := &AdminIdentity{
		Subject: "admin@example.com",
		Name:    "Admin User",
		Groups:  []string{"admins", "users"},
		IsAdmin: true,
	}

	verifier := &mockTokenVerifier{
		verifyFunc: func(ctx context.Context, token string) (*AdminIdentity, error) {
			if token != "valid-admin-token" {
				return nil, fmt.Errorf("unexpected token")
			}
			return expectedIdentity, nil
		},
	}

	var receivedIdentity *AdminIdentity
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedIdentity = GetIdentity(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := NewMiddleware(verifier)
	wrapped := mw.RequireAdmin(handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
	req.Header.Set("Authorization", "Bearer valid-admin-token")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for valid admin, got %d", rec.Code)
	}

	if receivedIdentity == nil {
		t.Fatal("identity should be set in context")
	}

	if receivedIdentity.Subject != expectedIdentity.Subject {
		t.Errorf("expected subject %q, got %q", expectedIdentity.Subject, receivedIdentity.Subject)
	}

	if !receivedIdentity.IsAdmin {
		t.Error("expected IsAdmin to be true")
	}
}

func TestGetIdentity_NoIdentityInContext(t *testing.T) {
	ctx := context.Background()
	identity := GetIdentity(ctx)

	if identity != nil {
		t.Errorf("expected nil identity, got %+v", identity)
	}
}

func TestAdminIdentity_JSON(t *testing.T) {
	identity := &AdminIdentity{
		Subject: "admin@example.com",
		Name:    "Admin User",
		Email:   "admin@example.com",
		Groups:  []string{"admins", "users"},
		IsAdmin: true,
	}

	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded AdminIdentity
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Subject != identity.Subject {
		t.Errorf("expected subject %q, got %q", identity.Subject, decoded.Subject)
	}

	if decoded.IsAdmin != identity.IsAdmin {
		t.Errorf("expected IsAdmin %v, got %v", identity.IsAdmin, decoded.IsAdmin)
	}

	if len(decoded.Groups) != len(identity.Groups) {
		t.Errorf("expected %d groups, got %d", len(identity.Groups), len(decoded.Groups))
	}
}
