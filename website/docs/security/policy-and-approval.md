---
title: Policy and Approval
---

# Policy and Approval

Before the runtime executes a tool, it runs a policy decision.

## Decision order

The current decision flow is:

1. **managed deny**: if the tool matches an internally tracked managed-scope deny pattern, reject with `managed_deny`
2. **curated view/tool set**: if the active mode is curated and the tool is outside the selected tool set, reject with `curated_deny`
3. **approval**: if the tool requires approval and the request did not grant it, reject with `approval_required`
4. otherwise allow execution

## Where approval requirements come from

A tool can require approval because of either:

- `x-cli-safety.requiresApproval` on the OpenAPI operation (possibly added by an overlay)
- a config pattern under `policy.approvalRequired`

CLI users grant approval with:

```bash
oascli --approval ...
```

HTTP API clients set:

```json
{ "approval": true }
```

## Example policy block

```json
{
  "mode": { "default": "curated" },
  "curation": {
    "toolSets": {
      "support": {
        "allow": ["tickets:listTickets", "tickets:getTicket", "tickets:createTicket"],
        "deny": ["**"]
      }
    }
  },
  "agents": {
    "profiles": {
      "support": {
        "mode": "curated",
        "toolSet": "support"
      }
    },
    "defaultProfile": "support"
  },
  "policy": {
    "approvalRequired": ["tickets:createTicket"]
  }
}
```

## Important implementation nuance: `policy.deny`

The config schema exposes `policy.deny`, and the loader merges those patterns. The current execution-time policy engine, however, checks only the managed-scope deny list directly.

Practical takeaway:

- use **managed scope policy** for hard organization-wide deny rules
- use **curated tool sets** for visibility and execution shaping
- use `approvalRequired` for interactive gates

Do not rely on project/user/local `policy.deny` as a hard enforcement layer in the current implementation.

## Pattern semantics

Pattern matching uses Go's `path.Match` plus a special `**` catch-all.

Examples:

- `tickets:*`
- `tickets:createTicket`
- `**`

For MCP-backed services, policy patterns are also validated at catalog-build time against `sources.<id>.disabledTools`. If an `approvalRequired`, managed deny, or curated tool-set pattern matches only MCP tools that were disabled, build fails closed. Broad patterns that still match at least one surviving tool remain valid.

## Audit behavior

Allowed and denied tool execution attempts are both written to the audit log. The denial reason becomes the audit `reasonCode`.
