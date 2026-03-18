---
title: Workflow Run
---

# Workflow Run

`ocli workflow run <workflow-id>` is a **validation and planning** command, not a workflow engine.

```bash
./bin/ocli --embedded --config ./.cli.json workflow run triageTicket --format pretty
```

## What it does

The CLI sends a request to `POST /v1/workflows/run`. The runtime then:

1. loads the catalog
2. finds the named workflow
3. checks that every step points to a tool that still exists
4. evaluates policy and approval requirements for each referenced tool
5. returns the workflow ID and the ordered list of step IDs

Example response:

```json
{
  "workflowId": "triageTicket",
  "steps": ["list-open", "fetch"]
}
```

## What it does **not** do

The runtime does **not** execute the tools in the workflow body. There is no built-in step runner, variable binding engine, or state machine.

Treat `workflow run` as:

- a way to confirm that the workflow still binds to real tools
- a way to confirm that the current mode/profile/policy allows the workflow
- a small planning primitive that a higher-level orchestrator can use

## How workflows bind to tools

At catalog build time, each workflow step may reference a tool by:

- `operationId`
- `operationPath` (for example `DELETE /tickets`)

Those references are converted into normalized tool IDs such as `tickets:listTickets`.

## Failure cases to expect

- `404 workflow not found` if the workflow ID does not exist
- `403 approval_required`, `403 curated_deny`, or `403 managed_deny` if policy blocks any referenced tool

A workflow with one invalid step is rejected as a whole.

Important nuance:

- if a workflow step points at an operation that no longer binds cleanly (for example an overlay marked it with `x-cli-ignore`), catalog build fails first with a step-specific error
- `workflow run` only sees workflows that already bound to real tools during catalog construction

## Authoring guidance

- Prefer stable `operationId` references over `operationPath`.
- Keep workflow IDs unique across all loaded workflow documents.
- Do not reference operations that overlays mark with `x-cli-ignore`; catalog build will fail.

For file format details, see [Arazzo workflows](../workflows-guidance/arazzo-workflows).
