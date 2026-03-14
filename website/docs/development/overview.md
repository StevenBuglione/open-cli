---
title: Development Overview
---

# Development Overview

The repository is organized around a clear split:

- **CLI surface** in `cmd/oascli`
- **runtime daemon** in `cmd/oasclird` and `internal/runtime`
- **library packages** under `pkg/` for config, discovery, catalog building, execution, caching, audit, instance handling, and observability
- **docs site** under `website/` for the published Docusaurus documentation and contributor guides

## Good contributor workflow

1. understand the user-facing behavior you are changing
2. find the package or docs section that owns that concern
3. update tests and docs close to that owner
4. run targeted `go test` commands while iterating
5. run `make verify` before handing off Go changes
6. if repo-facing docs changed, run `cd website && npm ci && npm run build`
7. if the change affects onboarding or contribution flow, update `README.md` and the relevant page under `website/docs/development/`

## When docs should move with the code

Update docs in the same change when you touch:

- CLI/runtime behavior that users see from commands or output
- configuration fields, defaults, policy, auth, or discovery semantics
- contributor workflow, repository layout, or verification expectations

## Most changes fall into one of these buckets

- config/schema changes
- discovery or catalog changes
- execution/auth changes
- runtime HTTP API changes
- CLI rendering and UX changes

Each bucket has a natural owner package; resist the urge to push behavior into `cmd/` when a reusable package should own it.

## Current design constraints worth respecting

- `oascli` is thin and runtime-backed by design
- the runtime is the enforcement point for policy, auth, cache, and audit
- instance isolation is file-system based
- observability is abstracted behind `pkg/obs.Observer`

Use the next pages for package-level detail.
