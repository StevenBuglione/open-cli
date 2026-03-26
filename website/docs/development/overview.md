---
title: Development Overview
---

# Development Overview

**Read this if** you are contributing to `open-cli` or evaluating the maturity of the implementation. This page answers: how is the repo organized, what is the contributor workflow, and why fleet validation and the product test harness are the primary signals that the implementation is production-ready — not just design intent.

The repository is organized around a clear split:

- **CLI surface** in `cmd/ocli`
- **hosted runtime** in `cmd/open-cli-toolbox` and `internal/runtime`
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

- `ocli` is thin and runtime-backed by design
- the runtime is the enforcement point for policy, auth, cache, and audit
- instance isolation is file-system based
- observability is abstracted behind `pkg/obs.Observer`

## Maturity signal: fleet validation

Contributors and evaluators can both read this the same way: the fleet validation matrix in `product-tests/testdata/fleet/capability-matrix.yaml` is executable evidence, not aspirational copy. It covers multi-session hosted runtime deployment, remote runtime auth, MCP transports, and real upstream API patterns. The live proof matrix covers flows that require external identity infrastructure.

See [Fleet validation](./fleet-validation) for the full picture.

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Understand the package ownership for a specific concern | [Repo layout](./repo-layout) |
| Run the reproducible fleet matrix locally | [Fleet validation](./fleet-validation) |
| Add or extend test coverage | [Testing](./testing) |
| Extend the runtime with new behavior | [Extending the runtime](./extending-the-runtime) |
| Verify the build before a handoff | Run `make verify` then `cd website && npm ci && npm run build` |

Use the next pages for package-level detail.
