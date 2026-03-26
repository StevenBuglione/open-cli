---
title: Workflows and Guidance Overview
---

# Workflows and Guidance Overview

**Read this if** you are an operator configuring skill manifests or Arazzo workflows to enrich the catalog for agents or users. This page answers: where these documents come from, what users and operators can actually do with them, and what the current implementation limits are.

This project supports two kinds of operator-supplied guidance on top of raw OpenAPI:

- **skill manifests** add usage guidance to tools
- **Arazzo workflows** group tools into named multi-step flows

Neither feature changes the upstream HTTP contract. They enrich the catalog that `ocli` and `open-cli-toolbox` work with.

## Where these documents come from

They can be attached in two ways:

- directly in `services.<id>.skills` and `services.<id>.workflows`
- indirectly through service metadata discovered from `service-meta` links

## What users see

- `explain <tool-id>` exposes skill guidance
- `tool schema <tool-id>` includes any merged guidance on the tool object
- `workflow run <workflow-id>` validates that a workflow still points to real, allowed tools

## What operators control

Operators decide:

- which tools get extra guidance
- which workflows are published
- which overlays rename or hide operations before those manifests are loaded
- which curated profiles can see those tools at all

## Key implementation limits

- skill manifests are simple JSON documents; there is no templating or inheritance engine
- workflows are validated and normalized, but not executed by the runtime
- if a workflow references an ignored or missing operation, catalog build fails

For MCP-backed services, that validation is now more specific: if a workflow step points at an operation that existed before `sources.<id>.disabledTools` filtering but now maps only to disabled MCP tools, catalog build fails with a disabled-tool-specific error instead of a generic missing-tool failure.

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Write a skill manifest for a service | [Skill manifests](./skill-manifests) |
| Define a multi-step workflow with Arazzo | [Arazzo workflows](./arazzo-workflows) |
| Understand how overlays interact with skill/workflow loading order | [Service discovery and overlays](../discovery-catalog/service-discovery-and-overlays) |
| Restrict which workflows a curated profile can see | [Modes and profiles](../configuration/modes-and-profiles) |
| Understand why catalog build fails on a missing workflow operation | See key implementation limits in this page |
| Get operator-focused guidance on combining these features | [Operator guidance](./operator-guidance) |

Continue with:

- [Skill manifests](./skill-manifests)
- [Arazzo workflows](./arazzo-workflows)
- [Operator guidance](./operator-guidance)
