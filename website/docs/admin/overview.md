# Admin Control-Plane Overview

The Open CLI admin control-plane provides centralized management of access packages (bundles), API sources, and user assignments. This guide covers the core administrative workflows.

## Quick Start

### Prerequisites

- Admin access token with `IsAdmin: true` claim
- Access to the admin API endpoint (e.g., `https://admin.example.com`)
- PostgreSQL database for persistence

### Basic Workflow

1. **Register API Sources**: Add OpenAPI specs or other API definitions
2. **Create Bundles**: Package API access into logical groups
3. **Assign Bundles**: Grant bundles to users or groups
4. **Publish Revisions**: Promote bundle configurations to production
5. **Monitor Audit Trail**: Track all administrative changes

## Managing API Sources

### Register a New Source

```bash
curl -X POST https://admin.example.com/v1/admin/sources \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "kind": "openapi",
    "displayName": "GitHub API"
  }'
```

Response:
```json
{
  "id": "src_123abc",
  "kind": "openapi",
  "displayName": "GitHub API",
  "status": "draft",
  "createdAt": "2024-03-26T10:00:00Z",
  "updatedAt": "2024-03-26T10:00:00Z"
}
```

### Validate a Source

Before publishing, validate that the source is correctly configured:

```bash
curl -X POST https://admin.example.com/v1/admin/sources/src_123abc/validate \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Response:
```json
{
  "sourceId": "src_123abc",
  "valid": true,
  "errors": [],
  "services": [
    {
      "name": "github-repos",
      "description": "GitHub repository operations",
      "endpoints": 15
    }
  ],
  "tools": [
    {
      "name": "create-repository",
      "description": "Create a new GitHub repository"
    }
  ]
}
```

### List Sources

```bash
curl https://admin.example.com/v1/admin/sources \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Managing Bundles

### Create a Bundle

Bundles group related API access for assignment to users or teams:

```bash
curl -X POST https://admin.example.com/v1/admin/bundles \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "engineering-tools",
    "description": "Standard tools for engineering team"
  }'
```

Response:
```json
{
  "id": "bundle_456def"
}
```

### List Bundles

```bash
curl https://admin.example.com/v1/admin/bundles \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### Update a Bundle

```bash
curl -X PUT https://admin.example.com/v1/admin/bundles/bundle_456def \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "engineering-tools-v2",
    "description": "Updated tools with new APIs"
  }'
```

### Delete a Bundle

```bash
curl -X DELETE https://admin.example.com/v1/admin/bundles/bundle_456def \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

**Note**: Deleting a bundle also removes all assignments. This action is logged in the audit trail.

## Managing Bundle Assignments

### Assign to a User

Grant a bundle to an individual user:

```bash
curl -X POST https://admin.example.com/v1/admin/bundles/bundle_456def/assignments \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "principal_type": "user",
    "principal_id": "engineer@example.com"
  }'
```

Response:
```json
{
  "id": "assignment_789ghi"
}
```

### Assign to a Group

Grant a bundle to an entire group:

```bash
curl -X POST https://admin.example.com/v1/admin/bundles/bundle_456def/assignments \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "principal_type": "group",
    "principal_id": "engineering-team"
  }'
```

### List Bundle Assignments

```bash
curl https://admin.example.com/v1/admin/bundles/bundle_456def/assignments \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Response:
```json
[
  {
    "id": "assignment_789ghi",
    "bundleId": "bundle_456def",
    "principalType": "user",
    "principalId": "engineer@example.com",
    "createdAt": "2024-03-26T10:30:00Z"
  },
  {
    "id": "assignment_101jkl",
    "bundleId": "bundle_456def",
    "principalType": "group",
    "principalId": "engineering-team",
    "createdAt": "2024-03-26T10:35:00Z"
  }
]
```

### Remove an Assignment

```bash
curl -X DELETE https://admin.example.com/v1/admin/assignments/assignment_789ghi \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Publishing Revisions

Revisions capture point-in-time snapshots of bundle configurations for versioned deployment.

### Create a Draft Revision

```bash
curl -X POST https://admin.example.com/v1/admin/bundles/bundle_456def/revisions \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "sources": ["src_123abc", "src_456def"]
  }'
```

### Publish a Revision

```bash
curl -X POST https://admin.example.com/v1/admin/revisions/rev_789/publish \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Published revisions are deployed to the runtime and become active for all users with the bundle assigned.

## Authentication and Authorization

### Admin Token Requirements

All admin API endpoints require:

- Valid Bearer token in `Authorization` header
- Token must have `IsAdmin: true` claim
- Token subject is logged in audit trail as `admin_id`

### Token Format

```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

### Permission Model

- **Admin Users**: Full CRUD access to all resources
- **Regular Users**: Denied access to admin endpoints (403 Forbidden)
- **Unauthenticated**: Denied access (401 Unauthorized)

## Audit and Compliance

All administrative actions are automatically logged. See [Admin Audit Trail](../operations/admin-audit-trail.md) for details.

### View Recent Changes

```bash
curl https://admin.example.com/v1/admin/audit/events?limit=20 \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### Filter by Action

```bash
curl "https://admin.example.com/v1/admin/audit/events?action=DELETE_BUNDLE" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### Filter by Admin

```bash
curl "https://admin.example.com/v1/admin/audit/events?admin_id=admin@example.com" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Common Workflows

### Onboard a New Team

1. Register API sources the team needs
2. Validate each source
3. Create a bundle for the team
4. Assign the bundle to the team's group
5. Verify in audit log

```bash
# 1. Register source
SOURCE_ID=$(curl -X POST .../sources -d '{"kind":"openapi","displayName":"Team API"}' | jq -r .id)

# 2. Validate
curl -X POST .../sources/$SOURCE_ID/validate

# 3. Create bundle
BUNDLE_ID=$(curl -X POST .../bundles -d '{"name":"team-bundle",...}' | jq -r .id)

# 4. Assign to group
curl -X POST .../bundles/$BUNDLE_ID/assignments -d '{"principal_type":"group","principal_id":"team-name"}'

# 5. Check audit
curl ".../audit/events?action=CREATE_BUNDLE&resource_id=$BUNDLE_ID"
```

### Update Access for a User

1. List current assignments for the user
2. Remove obsolete assignments
3. Add new assignments as needed
4. Verify changes in audit log

### Revoke Emergency Access

1. Identify the assignment to revoke
2. Delete the assignment
3. Confirm removal
4. Document in audit trail

```bash
# Find assignment
ASSIGNMENT_ID=$(curl .../bundles/bundle_456/assignments | jq -r '.[] | select(.principalId=="user@example.com") | .id')

# Revoke
curl -X DELETE .../assignments/$ASSIGNMENT_ID

# Verify audit
curl ".../audit/events?action=DELETE_ASSIGNMENT&resource_id=$ASSIGNMENT_ID"
```

## Best Practices

1. **Validate Before Publishing**: Always validate sources before creating revisions
2. **Use Groups**: Prefer group assignments over individual user assignments for maintainability
3. **Review Audit Trail**: Regularly review the audit log for unexpected changes
4. **Test Changes**: Use draft revisions to test configurations before publishing
5. **Document Bundles**: Use descriptive names and descriptions for bundles
6. **Monitor Failures**: Set up alerts for failed operations in the audit log

## Troubleshooting

### 401 Unauthorized

- Verify your admin token is valid and not expired
- Ensure the `Authorization` header uses `Bearer` prefix
- Check that the token has `IsAdmin: true` claim

### 403 Forbidden

- Your token is valid but lacks admin privileges
- Contact your administrator to grant admin access

### 404 Not Found

- The resource ID doesn't exist
- Check for typos in the resource ID
- Verify the resource wasn't deleted (check audit log)

### 500 Internal Server Error

- Database connection issue
- Check admin control-plane logs
- Verify PostgreSQL is running and accessible

## API Notes

The current admin surface is documented in this overview and in the admin product tests under `product-tests/tests/capability_admin_*_test.go`.

## See Also

- [Admin Audit Trail](../operations/admin-audit-trail.md)
- [Runtime Operations](../runtime/overview.md)
- [Security Overview](../security/overview.md)
