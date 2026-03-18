# Copilot Fleet Validation and Website Program Plan

## Status

This plan document is restored so earlier references in the repository resolve again.

For execution tracking and current closure status, use:

- `docs/superpowers/plans/2026-03-17-fleet-validation-gap-closure.md`

The gap-closure plan supersedes this file as the day-to-day execution tracker.

## Goal

Implement a validation and documentation program that proves `open-cli` across:

- local daemon mode
- remote runtime mode
- MCP transports
- remote API flows
- documented auth patterns
- first-run and enterprise-evaluation website paths

## Planned workstreams

### Product validation fleet

1. Add lane metadata to campaign rubrics.
2. Add matrix files for reproducible lanes and live proofs.
3. Implement executable campaign coverage for the highest-value runtime, MCP, and remote API paths.
4. Persist artifacts and transcripts so fleet output is reviewable.

### Website and onboarding review

1. Improve the docs IA for first-time users.
2. Add an enterprise-readiness bridge across deployment, security, proof, and operations pages.
3. Turn the website-review rubric into an executable campaign.

## Execution note

The implementation of this program continued in the gap-closure tracker because it became the practical place to record:

- missing proof
- truthful lane relabeling
- documentation corrections
- verification steps
- remaining closure work before merge

## Verification targets

The full program is considered closed only after running:

- `make verify`
- `make product-test-fleet`
- `cd product-tests && make fleet-matrix-mcp-remote`
- `make product-test-website-review`
- `cd website && npm run build`
