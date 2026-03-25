package helpers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRuntimeAuthBrokerDelegatedTokenExchangeIssuesShortLivedChild(t *testing.T) {
	t.Parallel()

	broker := NewRuntimeAuthBroker(t)
	parentToken := broker.AcquireClientCredentialsToken(t, "github", "oclird", []string{
		"bundle:tickets",
		"tool:tickets:listTickets",
	})

	childToken := broker.ExchangeDelegatedToken(t, parentToken, "oclird", []string{
		"tool:tickets:listTickets",
	}, "subagent:triage-01")

	claims, err := broker.validateRuntimeToken(childToken, "oclird")
	if err != nil {
		t.Fatalf("validate delegated token: %v", err)
	}
	if got := strings.Fields(claims.Scope); len(got) != 1 || got[0] != "tool:tickets:listTickets" {
		t.Fatalf("expected narrowed delegated scope, got %#v", got)
	}
	if claims.DelegatedBy != "github:service-account" {
		t.Fatalf("expected delegated_by to preserve parent principal, got %q", claims.DelegatedBy)
	}
	if claims.DelegationID == "" {
		t.Fatal("expected delegation_id on delegated token")
	}
	if claims.Act["client_id"] != broker.ClientID {
		t.Fatalf("expected act.client_id %q, got %#v", broker.ClientID, claims.Act)
	}
	if claims.Act["actor_id"] != "subagent:triage-01" {
		t.Fatalf("expected act.actor_id subagent:triage-01, got %#v", claims.Act)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("expected exp on delegated token")
	}
	if ttl := time.Until(claims.ExpiresAt.Time); ttl <= 0 || ttl > delegatedTokenTTL+15*time.Second {
		t.Fatalf("expected delegated ttl <= %s and >0, got %s", delegatedTokenTTL+15*time.Second, ttl)
	}
}

func TestRuntimeAuthBrokerDelegatedTokenExchangeRejectsScopeEscalation(t *testing.T) {
	t.Parallel()

	broker := NewRuntimeAuthBroker(t)
	parentToken := broker.AcquireClientCredentialsToken(t, "github", "oclird", []string{
		"bundle:tickets",
		"tool:tickets:listTickets",
	})

	form := url.Values{}
	form.Set("grant_type", tokenExchangeGrantType)
	form.Set("subject_token", parentToken)
	form.Set("subject_token_type", accessTokenType)
	form.Set("requested_token_type", accessTokenType)
	form.Set("audience", "oclird")
	form.Set("scope", "tool:users:listUsers")

	resp, err := http.PostForm(broker.TokenURL, form)
	if err != nil {
		t.Fatalf("post delegated exchange: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for delegated scope escalation, got %d", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode delegated exchange error: %v", err)
	}
	if payload["error"] != "invalid_scope" {
		t.Fatalf("expected invalid_scope, got %#v", payload)
	}
}
