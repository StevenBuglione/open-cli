package service

import (
	"context"
	"fmt"
	"time"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
	"github.com/google/uuid"
)

// LogAuditEvent logs an admin action to the audit trail
func (s *Service) LogAuditEvent(ctx context.Context, adminID, action, resourceType, resourceID string, changes map[string]interface{}, success bool, errorMsg string) error {
	event := domain.AdminAuditEvent{
		ID:           fmt.Sprintf("audit_%s", uuid.NewString()),
		Timestamp:    time.Now().UTC(),
		AdminID:      adminID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Changes:      changes,
		Success:      success,
		ErrorMessage: errorMsg,
	}

	return s.store.CreateAuditEvent(ctx, event)
}

// ListAuditEvents retrieves audit events based on filter criteria
func (s *Service) ListAuditEvents(ctx context.Context, filter domain.AuditEventFilter) ([]domain.AdminAuditEvent, error) {
	// Set default limit if not specified
	if filter.Limit == 0 {
		filter.Limit = 100
	}

	return s.store.ListAuditEvents(ctx, filter)
}

// GetAuditEvent retrieves a specific audit event by ID
func (s *Service) GetAuditEvent(ctx context.Context, id string) (*domain.AdminAuditEvent, error) {
	return s.store.GetAuditEvent(ctx, id)
}
