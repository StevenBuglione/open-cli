# Copilot Fleet Validation and Website Program Design

## Status

This design document is restored to preserve the original program reference path.

For execution tracking and closure status, use:

- `docs/superpowers/plans/2026-03-17-fleet-validation-gap-closure.md`

That gap-closure tracker is the operational source of truth for what was implemented, what remains, and what was verified.

## Problem

`open-cli` needed one program that could prove the product the way real operators use it while also making the docs site usable for both first-time users and enterprise evaluators.

Two failures had to be avoided:

- overclaiming CI coverage for flows that really require external identity or hosted infrastructure
- shipping documentation that is technically deep but hard to navigate as an onboarding or evaluation surface

## Design goals

- Use reproducible, executable fleet lanes wherever CI can honestly simulate the product path.
- Separate live external proof from baseline CI-safe coverage.
- Treat artifacts, transcripts, and rubric output as first-class evidence.
- Make the website expose a clear first-run path and an enterprise-readiness path without hiding the deeper system model.

## Program design

### 1. Capability matrix fleet

Use one matrix-driven validation program for reproducible product lanes, including:

- embedded and local-daemon runtime use
- remote runtime auth behavior
- MCP integrations
- remote API flows

Each lane emits structured rubric output so engineers and reviewers can inspect concrete pass/fail criteria and surviving artifacts.

### 2. Live proof inventory

Keep a separate live proof matrix for flows that cannot be claimed as CI-safe, such as:

- browser federation against real identity providers
- externally hosted remote runtime environments

These proofs should be evidence-driven and checklist-backed rather than pretending to be ordinary unit or product tests.

### 3. Website review workstream

Treat website review as a parallel validation workstream with bounded, executable IA checks first:

- onboarding entry points exist
- enterprise proof pages are surfaced
- docs bridge correctly between runtime, security, proof, and fleet evidence

Deeper content quality review can build on top of this structure later, but the first wave should verify paths and proof surfaces objectively.

## Outcome expected from this design

The program should leave the repository with:

- executable product fleet coverage for honest baseline paths
- explicit live-proof boundaries for enterprise-only scenarios
- persisted evidence artifacts from fleet runs
- docs that route first-time users and enterprise evaluators into the right pages quickly
