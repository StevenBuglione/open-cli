# OAS-CLI Agent Profile 0.1.0

## Skill Manifest

The OAS-CLI Skill Manifest is a machine-readable advisory document that maps tool IDs to usage guidance, caveats, and examples.

## Required Fields

- `oasCliSkill`
- `serviceId`
- `toolGuidance`

## Safety Metadata

Agent-facing metadata MUST be able to represent:

- `destructive`
- `readOnly`
- `requiresApproval`
- idempotency hints
- retry recommendations

## Workflow Binding

Workflows reference tools by operation identity and are exposed through the normalized catalog as resolved workflow steps.
