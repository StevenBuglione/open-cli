# OAS-CLI Core 0.1.0

## Scope

OAS-CLI standardizes how OpenAPI-described HTTP services become agent-usable CLI tools without introducing a separate tool protocol. The core specification defines:

- discovery inputs and provenance requirements
- normalized tool catalog structure
- runtime execution and policy enforcement rules
- audit and governance expectations

## Discovery

Implementations MUST support these source types:

- `apiCatalog`: RFC 9727 API catalogs represented as `application/linkset+json`
- `serviceRoot`: RFC 8631 service roots that advertise `service-desc` and `service-meta`
- `openapi`: direct references to OpenAPI documents

Implementations MUST record provenance for every fetched discovery document, including the URL, fetch timestamp, and discovery method.

## Normalized Tool Catalog

The runtime-to-CLI contract is the Normalized Tool Catalog (NTC). It MUST include:

- `catalogVersion`
- `generatedAt`
- `sourceFingerprint`
- `services`
- `tools`
- `effectiveViews`

Tool IDs MUST be stable across refetches of the same service. When `operationId` is present, it MUST be used as the operation identity component. Otherwise, implementations MUST derive an identity from method plus canonical path and mark it unstable.

## Runtime Requirements

The runtime MUST:

- apply overlays before OpenAPI indexing
- enforce managed policy denies before any network execution
- support curated mode and agent profile restrictions
- resolve secret references at execution time
- emit an audit event for every allowed or denied invocation attempt

## Governance

Policy enforcement order is:

1. merge scoped config
2. discover and normalize
3. compute effective view
4. apply managed denies
5. apply curated or agent restrictions
6. apply approval requirements
7. execute
