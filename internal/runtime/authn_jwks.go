package runtime

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/audit"
	"github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/golang-jwt/jwt/v5"
)

type oidcJWKSClaims struct {
	jwt.RegisteredClaims
	ClientID     string            `json:"client_id,omitempty"`
	Scope        string            `json:"scope,omitempty"`
	DelegatedBy  string            `json:"delegated_by,omitempty"`
	DelegationID string            `json:"delegation_id,omitempty"`
	Act          map[string]string `json:"act,omitempty"`
}

type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	KTY string `json:"kty"`
	Kid string `json:"kid,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
}

func runtimeServerAuthUsesOIDCJWKS(auth *config.RuntimeServerAuthConfig) bool {
	if auth == nil {
		return false
	}
	return auth.ValidationProfile == "oidc_jwks"
}

func (claims *oidcJWKSClaims) delegationLineage() *audit.DelegationLineage {
	if claims == nil {
		return nil
	}
	if claims.DelegatedBy == "" && claims.DelegationID == "" && len(claims.Act) == 0 {
		return nil
	}
	lineage := &audit.DelegationLineage{
		DelegatedBy:  claims.DelegatedBy,
		DelegationID: claims.DelegationID,
	}
	if len(claims.Act) > 0 {
		lineage.Actor = cloneNonEmptyStringMap(claims.Act)
	}
	return lineage
}

func (server *Server) validateJWTWithJWKS(ctx context.Context, authCfg config.RuntimeServerAuthConfig, rawToken string) (*oidcJWKSClaims, error) {
	claims := &oidcJWKSClaims{}
	options := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
	}
	if authCfg.Issuer != "" {
		options = append(options, jwt.WithIssuer(authCfg.Issuer))
	}
	if authCfg.Audience != "" {
		options = append(options, jwt.WithAudience(authCfg.Audience))
	}

	token, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		return server.fetchOIDCJWKSKey(ctx, authCfg.JWKSURL, kid)
	}, options...)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}
	return claims, nil
}

func (server *Server) fetchOIDCJWKSKey(ctx context.Context, jwksURL, kid string) (*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := server.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jwks fetch failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var document jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		return nil, err
	}
	key, err := document.lookupRSAKey(kid)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (document jwksDocument) lookupRSAKey(kid string) (*rsa.PublicKey, error) {
	if kid == "" && len(document.Keys) == 1 {
		return document.Keys[0].rsaPublicKey()
	}
	for _, key := range document.Keys {
		if key.Kid == kid {
			return key.rsaPublicKey()
		}
	}
	return nil, fmt.Errorf("jwks signing key not found")
}

func (key jwkKey) rsaPublicKey() (*rsa.PublicKey, error) {
	if key.KTY != "RSA" {
		return nil, fmt.Errorf("unsupported jwk key type %q", key.KTY)
	}
	modulus, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("decode jwk modulus: %w", err)
	}
	exponent, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("decode jwk exponent: %w", err)
	}

	n := new(big.Int).SetBytes(modulus)
	e := new(big.Int).SetBytes(exponent)
	if n.Sign() <= 0 || e.Sign() <= 0 {
		return nil, fmt.Errorf("invalid jwk rsa key")
	}

	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
