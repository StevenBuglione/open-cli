---
title: Config Schema
---

# Config Schema

The published JSON Schema for external validators lives in the companion spec repository:

- `oas-cli-spec/schemas/cli.schema.json`

This implementation also carries a local copy at `pkg/config/cli.schema.json`, and the loader embeds that copy for runtime validation. In practice, keep both in sync and treat the spec repository version as the public contract.

## Top-level fields

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `cli` | string | yes | Must be non-empty. |
| `mode` | object | yes | Requires `default`. |
| `sources` | object | yes | Must contain at least one source after final merge. |
| `services` | object | no | Optional named services. |
| `curation` | object | no | Tool sets for curated views. |
| `agents` | object | no | Profiles and default profile. |
| `policy` | object | no | Approval and secret-exec policy. |
| `secrets` | object | no | Secret references keyed by security scheme name. |

## `sources.*`

| Field | Type | Required | Values / meaning |
| --- | --- | --- | --- |
| `type` | string | yes | `apiCatalog`, `serviceRoot`, or `openapi` |
| `uri` | string | yes | URL or file/path reference depending on source type |
| `enabled` | boolean | no | Defaults to enabled for newly introduced sources |
| `refresh.maxAgeSeconds` | integer | no | Must be `>= 0` |
| `refresh.manualOnly` | boolean | no | Reuse cache until a forced refresh is requested |

## `services.*`

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `source` | string | yes | Source ID to bind to this service |
| `alias` | string | no | Top-level CLI command name |
| `overlays` | string[] | no | Overlay document refs |
| `skills` | string[] | no | Skill manifest refs |
| `workflows` | string[] | no | Arazzo workflow refs |

## `curation.toolSets.*`

| Field | Type | Meaning |
| --- | --- | --- |
| `allow` | string[] | If non-empty, only matching tools are visible in the tool set |
| `deny` | string[] | Matching tools are removed from the tool set |

## `agents`

| Field | Type | Meaning |
| --- | --- | --- |
| `defaultProfile` | string | Profile used when the active mode is curated and no explicit profile is provided |
| `profiles.*.mode` | string | `discover` or `curated` |
| `profiles.*.toolSet` | string | Name of a tool set under `curation.toolSets` |

## `policy`

| Field | Type | Meaning |
| --- | --- | --- |
| `deny` | string[] | Merged into effective config; see runtime nuance below |
| `approvalRequired` | string[] | Tool patterns that require approval |
| `allowExecSecrets` | boolean | Enables `exec` secret resolution |

## `secrets.*`

| Field | Type | Meaning |
| --- | --- | --- |
| `type` | string | `env`, `file`, `osKeychain`, or `exec` |
| `value` | string | Env var name, file path, keychain ref, or fallback command |
| `command` | string[] | Explicit argv for `exec` secrets |

## Loader behavior that matters in practice

### JSON only

Config files are decoded with Go's JSON decoder. `.cli.json` is literal JSON, not YAML.

### Unknown fields are rejected

The loader calls `DisallowUnknownFields()` during JSON decoding. In practice, that means stray keys still fail even in places where the JSON Schema is intentionally permissive for forward compatibility.

### Cross-reference validation exists

After schema validation, the loader also checks that every `services.<id>.source` points at a known source.

### Final validation is stricter than per-scope validation

Individual scope files may omit required top-level fields. The final merged config may not.

## Runtime nuance: `policy.deny`

The schema exposes `policy.deny`, and the loader merges it into the effective config. The current execution-time policy engine, however, only enforces the internally tracked managed deny list directly. Use curated tool sets and managed scope policy for hard deny behavior in the current release.
