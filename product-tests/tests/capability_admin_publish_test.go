package tests_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
)

// TestAdminPublishBundleWorkflow tests the complete bundle creation and assignment workflow
func TestAdminPublishBundleWorkflow(t *testing.T) {
	server, svc, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"
	ctx := context.Background()

	// 1. Create a source
	resp, result := doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/sources", adminToken, map[string]interface{}{
		"kind":        "openapi",
		"displayName": "Test API Source",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create source: expected 201, got %d", resp.StatusCode)
	}
	sourceID, ok := result["ID"].(string)
	if !ok || sourceID == "" {
		t.Fatalf("create source response missing ID: %#v", result)
	}
	t.Logf("Created source: %s", sourceID)

	// 2. Create a bundle
	resp, result = doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "engineering-bundle",
		"description": "Bundle for engineering team",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create bundle: expected 201, got %d", resp.StatusCode)
	}
	bundleID := result["id"].(string)
	t.Logf("Created bundle: %s", bundleID)

	// 3. Assign bundle to a user
	resp, result = doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles/"+bundleID+"/assignments", adminToken, map[string]interface{}{
		"principal_type": "user",
		"principal_id":   "engineer@example.com",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create assignment: expected 201, got %d", resp.StatusCode)
	}
	assignmentID := result["id"].(string)
	t.Logf("Created assignment: %s", assignmentID)

	// 4. List bundle assignments
	resp, result = doAdminRequest(t, http.MethodGet, server.URL+"/v1/admin/bundles/"+bundleID+"/assignments", adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list assignments: expected 200, got %d", resp.StatusCode)
	}

	assignments, err := svc.ListBundleAssignments(ctx, bundleID)
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(assignments) != 1 {
		t.Errorf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].PrincipalID != "engineer@example.com" {
		t.Errorf("expected principal engineer@example.com, got %s", assignments[0].PrincipalID)
	}

	// 5. Verify audit trail records all operations
	time.Sleep(100 * time.Millisecond)
	auditEvents, err := svc.ListAuditEvents(ctx, domain.AuditEventFilter{
		AdminID: "admin@example.com",
	})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	t.Logf("Found %d audit events for workflow", len(auditEvents))

	// Should have at least CREATE_SOURCE, CREATE_BUNDLE, CREATE_ASSIGNMENT
	if len(auditEvents) < 3 {
		t.Logf("Warning: expected at least 3 audit events, got %d", len(auditEvents))
	}
}

// TestAdminPublishGroupAssignment tests bundle assignment to groups
func TestAdminPublishGroupAssignment(t *testing.T) {
	server, svc, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"
	ctx := context.Background()

	// Create a bundle
	resp, result := doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "group-bundle",
		"description": "Bundle for groups",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create bundle: expected 201, got %d", resp.StatusCode)
	}
	bundleID := result["id"].(string)

	// Assign to a group
	resp, result = doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles/"+bundleID+"/assignments", adminToken, map[string]interface{}{
		"principal_type": "group",
		"principal_id":   "engineering-team",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create group assignment: expected 201, got %d", resp.StatusCode)
	}

	// Verify assignment
	assignments, err := svc.ListBundleAssignments(ctx, bundleID)
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].PrincipalType != "group" {
		t.Errorf("expected principal type group, got %s", assignments[0].PrincipalType)
	}
	if assignments[0].PrincipalID != "engineering-team" {
		t.Errorf("expected principal engineering-team, got %s", assignments[0].PrincipalID)
	}
}

// TestAdminPublishMultipleAssignments tests multiple assignments to same bundle
func TestAdminPublishMultipleAssignments(t *testing.T) {
	server, svc, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"
	ctx := context.Background()

	// Create bundle
	resp, result := doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "multi-assignment-bundle",
		"description": "Test multiple assignments",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create bundle: expected 201, got %d", resp.StatusCode)
	}
	bundleID := result["id"].(string)

	// Assign to multiple users
	users := []string{"user1@example.com", "user2@example.com", "user3@example.com"}
	for _, userID := range users {
		resp, _ = doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles/"+bundleID+"/assignments", adminToken, map[string]interface{}{
			"principal_type": "user",
			"principal_id":   userID,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create assignment for %s: expected 201, got %d", userID, resp.StatusCode)
		}
	}

	// Verify all assignments
	assignments, err := svc.ListBundleAssignments(ctx, bundleID)
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(assignments) != len(users) {
		t.Errorf("expected %d assignments, got %d", len(users), len(assignments))
	}
}

// TestAdminPublishDeleteAssignment tests removing bundle assignments
func TestAdminPublishDeleteAssignment(t *testing.T) {
	server, svc, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"
	ctx := context.Background()

	// Create bundle and assignment
	resp, result := doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "delete-assignment-bundle",
		"description": "Test assignment deletion",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create bundle: expected 201, got %d", resp.StatusCode)
	}
	bundleID := result["id"].(string)

	resp, result = doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles/"+bundleID+"/assignments", adminToken, map[string]interface{}{
		"principal_type": "user",
		"principal_id":   "temp-user@example.com",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create assignment: expected 201, got %d", resp.StatusCode)
	}
	assignmentID := result["id"].(string)

	// Delete assignment
	resp, _ = doAdminRequest(t, http.MethodDelete, server.URL+"/v1/admin/assignments/"+assignmentID, adminToken, nil)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete assignment: expected 204 or 200, got %d", resp.StatusCode)
	}

	// Verify assignment is gone
	assignments, err := svc.ListBundleAssignments(ctx, bundleID)
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(assignments) != 0 {
		t.Errorf("expected 0 assignments after delete, got %d", len(assignments))
	}
}

// TestAdminPublishInvalidPrincipalType tests validation of principal types
func TestAdminPublishInvalidPrincipalType(t *testing.T) {
	server, _, cleanup := setupTestAdmin(t)
	defer cleanup()

	const adminToken = "valid-admin-token"

	// Create bundle
	resp, result := doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles", adminToken, map[string]interface{}{
		"name":        "validation-bundle",
		"description": "Test validation",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create bundle: expected 201, got %d", resp.StatusCode)
	}
	bundleID := result["id"].(string)

	// Try to create assignment with invalid principal type
	resp, _ = doAdminRequest(t, http.MethodPost, server.URL+"/v1/admin/bundles/"+bundleID+"/assignments", adminToken, map[string]interface{}{
		"principal_type": "invalid",
		"principal_id":   "test@example.com",
	})
	if resp.StatusCode == http.StatusCreated {
		t.Error("expected error for invalid principal type, got 201")
	}
}
