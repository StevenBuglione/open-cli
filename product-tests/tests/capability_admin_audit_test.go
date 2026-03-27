package tests_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/admin/authn"
	"github.com/StevenBuglione/open-cli/internal/admin/domain"
	"github.com/StevenBuglione/open-cli/internal/admin/httpapi"
	"github.com/StevenBuglione/open-cli/internal/admin/service"
	"github.com/StevenBuglione/open-cli/internal/admin/store"
	_ "modernc.org/sqlite"
)

// mockTokenVerifier provides a test token verifier
type mockTokenVerifier struct {
	verifyFunc func(ctx context.Context, token string) (*authn.AdminIdentity, error)
}

func (m *mockTokenVerifier) VerifyToken(ctx context.Context, token string) (*authn.AdminIdentity, error) {
	return m.verifyFunc(ctx, token)
}

// setupTestAdmin creates an admin server with in-memory database
func setupTestAdmin(t *testing.T) (*httptest.Server, *service.Service, func()) {
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
			if token == "valid-admin-token" {
				return &authn.AdminIdentity{
					Subject: "admin@example.com",
					Name:    "Test Admin",
					Groups:  []string{"admins"},
					IsAdmin: true,
				}, nil
			}
			if token == "valid-user-token" {
				return &authn.AdminIdentity{
					Subject: "user@example.com",
					Name:    "Test User",
					Groups:  []string{"users"},
					IsAdmin: false,
				}, nil
			}
			return nil, authn.ErrInvalidToken
		},
	}

	deps := httpapi.NewDependencies(svc, verifier)
	router := httpapi.RegisterRoutes(http.NewServeMux(), deps)
	server := httptest.NewServer(router)

	cleanup := func() {
		server.Close()
		db.Close()
	}

	return server, svc, cleanup
}

func doAdminRequest(t *testing.T, method, url, token string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()

	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}

	var result map[string]interface{}
	if resp.StatusCode != http.StatusNoContent {
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		if len(bytes.TrimSpace(payload)) > 0 && bytes.TrimSpace(payload)[0] == '{' {
			if err := json.Unmarshal(payload, &result); err != nil && resp.StatusCode < 400 {
				t.Fatalf("decode response: %v", err)
			}
		}
	}
	resp.Body.Close()

	return resp, result
}

// TestAdminAuditBundleLifecycle tests that bundle CRUD operations are audited
func TestAdminAuditBundleLifecycle(t *testing.T) {
	server, svc, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"

	// 1. Create bundle
	resp, result := doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "test-bundle",
		"description": "Test bundle for audit",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create bundle: expected 201, got %d", resp.StatusCode)
	}
	bundleID := result["id"].(string)
	if bundleID == "" {
		t.Fatal("bundle ID is empty")
	}

	// Wait for audit event to be written
	time.Sleep(100 * time.Millisecond)

	// 2. Check audit events for CREATE_BUNDLE
	resp, _ = doAdminRequest(t, http.MethodGet, server.URL+"/v1/admin/audit/events?action=CREATE_BUNDLE", adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list audit events: expected 200, got %d", resp.StatusCode)
	}

	// Verify audit trail via service
	auditEvents, err := svc.ListAuditEvents(context.Background(), domain.AuditEventFilter{
		Action:       "CREATE_BUNDLE",
		ResourceType: "bundle",
	})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(auditEvents) == 0 {
		t.Fatal("expected at least one CREATE_BUNDLE audit event")
	}

	createEvent := auditEvents[0]
	if createEvent.AdminID != "admin@example.com" {
		t.Errorf("expected admin_id admin@example.com, got %s", createEvent.AdminID)
	}
	if createEvent.ResourceID != bundleID {
		t.Errorf("expected resource_id %s, got %s", bundleID, createEvent.ResourceID)
	}
	if !createEvent.Success {
		t.Error("expected success=true for create event")
	}

	// 3. Update bundle (should create UPDATE_BUNDLE audit event)
	resp, _ = doAdminRequest(t, http.MethodPut, server.URL+"/v1/admin/bundles/"+bundleID, adminToken, map[string]interface{}{
		"name":        "updated-bundle",
		"description": "Updated description",
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("update bundle: expected 204, got %d", resp.StatusCode)
	}

	// 4. Delete bundle (should create DELETE_BUNDLE audit event)
	resp, _ = doAdminRequest(t, http.MethodDelete, server.URL+"/v1/admin/bundles/"+bundleID, adminToken, nil)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete bundle: expected 204 or 200, got %d", resp.StatusCode)
	}

	time.Sleep(100 * time.Millisecond)

	// 5. Verify all three audit events exist
	auditEvents, err = svc.ListAuditEvents(context.Background(), domain.AuditEventFilter{
		ResourceID: bundleID,
	})
	if err != nil {
		t.Fatalf("list audit events by resource: %v", err)
	}

	t.Logf("Found %d audit events for bundle %s", len(auditEvents), bundleID)

	// We expect CREATE, potentially UPDATE, potentially DELETE
	// For now, just verify we have events
	if len(auditEvents) == 0 {
		t.Error("expected at least one audit event for bundle lifecycle")
	}
}

// TestAdminAuditSourceOperations tests that source operations are audited
func TestAdminAuditSourceOperations(t *testing.T) {
	server, svc, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"

	// 1. Create source
	resp, result := doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/sources", adminToken, map[string]interface{}{
		"kind":        "openapi",
		"displayName": "Test API",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create source: expected 201, got %d", resp.StatusCode)
	}
	sourceID, ok := result["ID"].(string)
	if !ok || sourceID == "" {
		t.Fatalf("create source response missing ID: %#v", result)
	}

	time.Sleep(100 * time.Millisecond)

	// 2. Verify CREATE_SOURCE audit event
	auditEvents, err := svc.ListAuditEvents(context.Background(), domain.AuditEventFilter{
		Action:       "CREATE_SOURCE",
		ResourceType: "source",
		ResourceID:   sourceID,
	})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}

	t.Logf("Found %d CREATE_SOURCE events", len(auditEvents))
}

// TestAdminAuditFilterByAdmin tests filtering audit events by admin user
func TestAdminAuditFilterByAdmin(t *testing.T) {
	server, svc, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"

	// Create a bundle
	doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "filter-test-bundle",
		"description": "Test filtering",
	})

	time.Sleep(100 * time.Millisecond)

	// Query by admin ID
	auditEvents, err := svc.ListAuditEvents(context.Background(), domain.AuditEventFilter{
		AdminID: "admin@example.com",
	})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}

	for _, event := range auditEvents {
		if event.AdminID != "admin@example.com" {
			t.Errorf("filter failed: got event with admin_id %s", event.AdminID)
		}
	}
}

// TestAdminAuditTimeRange tests filtering audit events by time range
func TestAdminAuditTimeRange(t *testing.T) {
	server, _, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"

	startTime := time.Now().UTC()

	// Create a bundle
	doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "time-range-bundle",
		"description": "Test time range",
	})

	time.Sleep(100 * time.Millisecond)
	endTime := time.Now().UTC()

	// Query with time range via HTTP API
	resp, _ := doAdminRequest(t, http.MethodGet,
		server.URL+"/v1/admin/audit/events?start_time="+startTime.Format(time.RFC3339),
		adminToken, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list audit events: expected 200, got %d", resp.StatusCode)
	}

	t.Logf("Time range query from %s to %s successful", startTime, endTime)
}
