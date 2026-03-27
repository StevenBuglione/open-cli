# Admin Control-Plane Audit Trail

The Open CLI admin control-plane maintains a comprehensive audit trail of all administrative actions for compliance, security, and change tracking purposes.

## Overview

Every mutating operation performed through the admin API is automatically logged to an audit trail stored in the PostgreSQL database. This provides a complete history of:

- Who made changes (admin user identity)
- What changes were made (action type and details)
- When changes occurred (timestamp)
- Whether the operation succeeded or failed
- What resource was affected (bundle, source, assignment, etc.)

## Audit Event Structure

Each audit event contains the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for the audit event (e.g., `audit_123e4567-e89b-12d3-a456-426614174000`) |
| `timestamp` | timestamp | UTC timestamp when the action occurred |
| `admin_id` | string | Subject/email of the admin user who performed the action |
| `action` | string | Type of action performed (see Action Types below) |
| `resource_type` | string | Type of resource affected (`bundle`, `source`, `assignment`, `revision`) |
| `resource_id` | string | ID of the affected resource |
| `changes` | object | Details of the changes made (field values, old/new values) |
| `success` | boolean | Whether the operation completed successfully |
| `error_message` | string | Error details if the operation failed |

## Action Types

The following action types are recorded in the audit trail:

### Bundle Actions
- `CREATE_BUNDLE` - A new bundle was created
- `UPDATE_BUNDLE` - An existing bundle was modified
- `DELETE_BUNDLE` - A bundle was deleted

### Source Actions
- `CREATE_SOURCE` - A new API source was registered
- `VALIDATE_SOURCE` - A source was validated
- `UPDATE_SOURCE` - Source configuration was modified
- `DELETE_SOURCE` - A source was removed

### Assignment Actions
- `CREATE_ASSIGNMENT` - A bundle was assigned to a user or group
- `DELETE_ASSIGNMENT` - A bundle assignment was removed

### Publish Actions
- `CREATE_REVISION` - A new bundle revision was created
- `PUBLISH_REVISION` - A revision was published to production

## Querying Audit Events

### Via API

Query audit events through the admin API:

```bash
# List all recent audit events
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://admin.example.com/v1/admin/audit/events

# Filter by admin user
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://admin.example.com/v1/admin/audit/events?admin_id=admin@example.com

# Filter by action type
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://admin.example.com/v1/admin/audit/events?action=CREATE_BUNDLE

# Filter by resource
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://admin.example.com/v1/admin/audit/events?resource_type=bundle&resource_id=bundle_abc123

# Pagination
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://admin.example.com/v1/admin/audit/events?limit=50&offset=0
```

### Via Database

For advanced analysis or reporting, query the `admin_audit_events` table directly:

```sql
-- Recent bundle deletions
SELECT timestamp, admin_id, resource_id, changes
FROM admin_audit_events
WHERE action = 'DELETE_BUNDLE'
  AND timestamp > NOW() - INTERVAL '7 days'
ORDER BY timestamp DESC;

-- Actions by a specific admin
SELECT action, resource_type, resource_id, success
FROM admin_audit_events
WHERE admin_id = 'admin@example.com'
ORDER BY timestamp DESC
LIMIT 100;

-- Failed operations
SELECT timestamp, admin_id, action, resource_type, error_message
FROM admin_audit_events
WHERE success = false
ORDER BY timestamp DESC;
```

## Example Audit Events

### Successful Bundle Creation

```json
{
  "id": "audit_123e4567-e89b-12d3-a456-426614174000",
  "timestamp": "2024-03-26T10:30:00Z",
  "admin_id": "admin@example.com",
  "action": "CREATE_BUNDLE",
  "resource_type": "bundle",
  "resource_id": "bundle_abc123",
  "changes": {
    "name": "engineering-tools",
    "description": "Tools for engineering team"
  },
  "success": true,
  "error_message": ""
}
```

### Failed Bundle Assignment

```json
{
  "id": "audit_789e4567-e89b-12d3-a456-426614174999",
  "timestamp": "2024-03-26T10:35:00Z",
  "admin_id": "admin@example.com",
  "action": "CREATE_ASSIGNMENT",
  "resource_type": "assignment",
  "resource_id": "",
  "changes": {
    "bundle_id": "bundle_nonexistent",
    "principal_type": "user",
    "principal_id": "user@example.com"
  },
  "success": false,
  "error_message": "bundle not found"
}
```

## Compliance and Retention

### Audit Trail Properties

- **Immutable**: Audit events cannot be modified or deleted through the API
- **Comprehensive**: All mutating admin operations are logged automatically
- **Timestamped**: All timestamps are stored in UTC for consistency
- **Indexed**: Efficient queries by admin, action, resource, and timestamp

### Retention Policy

The audit trail is retained in the PostgreSQL database. Implement your organization's retention policy through database-level archival or deletion:

```sql
-- Archive old audit events (example)
CREATE TABLE admin_audit_events_archive AS
SELECT * FROM admin_audit_events
WHERE timestamp < NOW() - INTERVAL '1 year';

DELETE FROM admin_audit_events
WHERE timestamp < NOW() - INTERVAL '1 year';
```

### Access Control

- Audit event queries require admin authentication (Bearer token)
- Only users with `IsAdmin: true` in their token can access audit events
- Consider additional RBAC controls for sensitive audit access

## Integration with Runtime Audit

The admin control-plane audit trail is separate from but complementary to the [runtime audit logging](./audit-logging.md):

- **Admin Audit**: Tracks control-plane changes (bundles, sources, assignments)
- **Runtime Audit**: Tracks operator actions (tool execution, policy decisions, API calls)

For complete observability, correlate both audit trails:

1. Admin changes bundle assignment → Admin audit logs `CREATE_ASSIGNMENT`
2. User receives new tool access → Runtime audit logs `tool_execution`
3. Trace access back to admin decision via bundle assignment

## Best Practices

1. **Regular Review**: Audit admin actions regularly for security and compliance
2. **Alerting**: Monitor for suspicious patterns (e.g., mass deletions, failed access attempts)
3. **Correlation**: Link admin changes to runtime behavior for complete observability
4. **Backup**: Include audit trail in database backup and disaster recovery plans
5. **Export**: Export audit events to SIEM or log aggregation systems for long-term storage

## See Also

- [Runtime Audit Logging](./audit-logging.md)
- [Security Overview](../security/overview.md)
- [Authorization Resolution](../security/auth-resolution.md)
