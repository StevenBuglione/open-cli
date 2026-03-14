---
title: Workflows and Guidance Overview
---

# Workflows and Guidance Overview

This project supports two kinds of operator-supplied guidance on top of raw OpenAPI:

- **skill manifests** add usage guidance to tools
- **Arazzo workflows** group tools into named multi-step flows

Neither feature changes the upstream HTTP contract. They enrich the catalog that `oascli` and `oasclird` work with.

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

Continue with:

- [Skill manifests](./skill-manifests)
- [Arazzo workflows](./arazzo-workflows)
- [Operator guidance](./operator-guidance)
