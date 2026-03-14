package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultTTLSeconds = 3600
	shortTTLSeconds   = 2 // for refresh-testing scenarios
)

// tokenRecord holds an issued access token and its metadata.
type tokenRecord struct {
	token     string
	clientID  string
	scopes    []string
	expiresAt time.Time
}

// Server implements a minimal OAuth 2.0 authorization server stub.
type Server struct {
	store  *Store
	mu     sync.Mutex
	tokens map[string]*tokenRecord
	issuer string
}

// New creates a new Server backed by the provided Store.
// issuer is the base URL used in the discovery document.
func New(store *Store, issuer string) *Server {
	return &Server{
		store:  store,
		tokens: make(map[string]*tokenRecord),
		issuer: issuer,
	}
}

// Handler returns an http.Handler for the OAuth stub.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleDiscovery)
	mux.HandleFunc("/oauth/token", s.handleToken)
	return mux
}

// handleDiscovery serves the RFC 8414 authorization server metadata document.
func (s *Server) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"issuer":                                s.issuer,
		"token_endpoint":                        s.issuer + "/oauth/token",
		"grant_types_supported":                 []string{"client_credentials"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
		"scopes_supported":                      []string{"api.read", "api.write"},
		"response_types_supported":              []string{"token"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleToken processes token requests (grant_type=client_credentials only).
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		tokenError(w, "invalid_request", "cannot parse form")
		return
	}

	grantType := r.FormValue("grant_type")
	if grantType != "client_credentials" {
		tokenError(w, "unsupported_grant_type", "only client_credentials is supported")
		return
	}

	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	client := s.store.Lookup(clientID)
	if client == nil || client.Secret != clientSecret {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_client",
			"error_description": "unknown client or bad credentials",
		})
		return
	}

	rawScope := r.FormValue("scope")
	var requestedScopes []string
	if rawScope != "" {
		requestedScopes = strings.Fields(rawScope)
	}
	if len(requestedScopes) > 0 && !s.store.ValidateScopes(client, requestedScopes) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_scope",
			"error_description": "one or more requested scopes are not allowed for this client",
		})
		return
	}

	ttl := defaultTTLSeconds
	if clientID == "short-ttl-client" {
		ttl = shortTTLSeconds
	}

	token := mustRandHex(16)
	rec := &tokenRecord{
		token:     token,
		clientID:  clientID,
		scopes:    requestedScopes,
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}
	s.mu.Lock()
	s.tokens[token] = rec
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   ttl,
	})
}

func tokenError(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}

func mustRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
