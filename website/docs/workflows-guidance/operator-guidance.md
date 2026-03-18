---
title: Operator Guidance
---

# Operator Guidance

Operators decide what the CLI experience looks like long before end users type a command.

## Recommended publishing layers

### 1. Start with an accurate OpenAPI document

Make sure these are stable first:

- `operationId`
- tags
- server URLs
- security scheme names
- request body schemas

Those fields drive tool IDs, grouping, auth lookup, and request-body metadata.

### 2. Add overlays for CLI shaping

Use overlays to:

- rename awkward commands (`x-cli-name`)
- rename flags (`x-cli-name` on parameters)
- mark sensitive actions with `x-cli-safety.requiresApproval`
- hide or ignore operations that should not surface in the CLI

### 3. Add skill manifests for human guidance

Skill manifests are the best place to explain when a tool is appropriate and provide example commands.

### 4. Add workflows for common multi-step flows

Use workflows when you want a named, reviewable path through several tools, even if your higher-level orchestrator executes the steps elsewhere.

## Publishing through service metadata

For `serviceRoot` discovery, publish both `service-desc` and `service-meta` links:

```http
Link: <https://api.example.com/openapi.json>; rel="service-desc", <https://api.example.com/metadata/linkset.json>; rel="service-meta"
```

Then publish metadata like:

```json
{
  "linkset": [
    {
      "href": "./overlays/tickets.overlay.yaml",
      "rel": "https://open-cli.dev/rel/schema-overlay"
    },
    {
      "href": "./skills/tickets.skill.json",
      "rel": "https://open-cli.dev/rel/skill-manifest"
    },
    {
      "href": "./workflows/tickets.arazzo.yaml",
      "rel": "https://open-cli.dev/rel/workflows"
    }
  ]
}
```

## Publishing through an API catalog

Use an API catalog when you want one organization-level entrypoint that fans out to many service roots.

That is especially useful when:

- services are maintained by different teams
- consumers should start from one well-known URL
- you want discovery to pick up new services without editing every client config

## Curation strategy that works well today

A practical setup is:

- managed scope for organization-wide hard deny rules
- project scope for named services, overlays, skills, and workflows
- curated tool sets per team or persona
- `approvalRequired` for sensitive but still available actions

## Security checklist

- keep security scheme names stable so `secrets` mappings do not break
- prefer env/file/keychain secrets over `exec` unless necessary
- bind `oclird` to localhost unless you have external protections
- test both `catalog list` and a real tool execution after changing auth or curation
