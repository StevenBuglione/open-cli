---
title: Arazzo Workflows
---

# Arazzo Workflows

The catalog builder can load Arazzo-like workflow documents and normalize them into workflow IDs plus step-to-tool bindings.

## File format used by the current implementation

```yaml
arazzo: 1.0.0
info:
  title: Ticket workflows
  version: 1.0.0
workflows:
  - workflowId: triageTicket
    steps:
      - stepId: list-open
        operationId: listTickets
      - stepId: fetch
        operationId: getTicket
```

Each step may reference a tool by either:

- `operationId`
- `operationPath` (for example `DELETE /tickets`)

## Binding rules

During catalog build:

1. OpenAPI operations are normalized into tool IDs
2. each workflow step is matched to a tool by `operationId` or `operationPath`
3. the stored workflow step becomes `{stepId, toolId}`

If no matching tool exists, build fails with a step-specific error.

## Interaction with overlays

Overlays can change workflow outcomes indirectly:

- renaming commands does **not** break workflow binding because binding uses operation identity, not CLI name
- `x-cli-ignore` does break workflows if a step pointed at that operation

That behavior is covered by repository tests and is worth planning around when curating APIs.

## What `workflow run` does with these files

The runtime does not execute Arazzo steps. Instead it:

- confirms the workflow ID exists
- checks that every bound tool is still present
- evaluates policy and approval for every referenced tool
- returns the ordered list of step IDs

## Recommended authoring style

- prefer `operationId` over `operationPath`
- keep `workflowId` values stable because the CLI refers to them directly
- keep step IDs human-readable because they are what `workflow run` returns
