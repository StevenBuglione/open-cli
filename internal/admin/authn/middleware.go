package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	// ErrInvalidToken is returned when a token is invalid or verification fails
	ErrInvalidToken = errors.New("invalid token")
)

type contextKey int

const identityKey contextKey = 0

// AdminIdentity represents the authenticated admin user
type AdminIdentity struct {
	Subject string   `json:"subject"`
	Name    string   `json:"name,omitempty"`
	Email   string   `json:"email,omitempty"`
	Groups  []string `json:"groups,omitempty"`
	IsAdmin bool     `json:"is_admin"`
}

// TokenVerifier validates admin tokens and returns identity information
type TokenVerifier interface {
	VerifyToken(ctx context.Context, token string) (*AdminIdentity, error)
}

// Middleware provides authentication and authorization middleware for admin endpoints
type Middleware struct {
	verifier TokenVerifier
}

// NewMiddleware creates a new admin authentication middleware
func NewMiddleware(verifier TokenVerifier) *Middleware {
	return &Middleware{
		verifier: verifier,
	}
}

// RequireAdmin wraps an HTTP handler to require admin authentication and authorization
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := extractBearerToken(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		identity, err := m.verifier.VerifyToken(r.Context(), token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if !identity.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), identityKey, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetIdentity retrieves the authenticated admin identity from the request context
func GetIdentity(ctx context.Context) *AdminIdentity {
	identity, _ := ctx.Value(identityKey).(*AdminIdentity)
	return identity
}

// extractBearerToken extracts the bearer token from the Authorization header
func extractBearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", fmt.Errorf("missing bearer token")
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("invalid bearer token")
	}

	return strings.TrimSpace(parts[1]), nil
}
