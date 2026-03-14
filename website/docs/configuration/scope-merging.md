---
title: Scope Merging
---

# Scope Merging

The loader can merge up to four config scopes into one effective config.

## Scope discovery locations

Unless you override paths programmatically, `pkg/config.DiscoverScopePaths` looks here:

| Scope | Default path |
| --- | --- |
| Managed | `/etc/oas-cli/.cli.json` |
| User | `$XDG_CONFIG_HOME/oas-cli/.cli.json` or `~/.config/oas-cli/.cli.json` |
| Project | `<working-dir>/.cli.json` |
| Local | `<working-dir>/.cli.local.json` |

`oasclird` and the runtime API typically load a project config by explicit path, but the same merge rules still apply inside the config package.

## Load order

Scopes merge in this order:

1. managed
2. user
3. project
4. local

Later scopes override or extend earlier scopes depending on the field type.

## Validation model

Each scope file is validated with a **relaxed** schema first:

- required fields are not enforced per-scope
- `minProperties` checks are relaxed per-scope

Then the final merged config is validated strictly.

That means a local override file can safely contain only the keys it wants to change.

## Exact merge rules

| Field | Merge behavior |
| --- | --- |
| `cli` | last non-empty value wins |
| `mode.default` | last non-empty value wins |
| `sources` | merged per source ID; individual fields override prior values |
| `sources.*.refresh` | replaced as a whole when present in a later scope |
| `services` | merged per service ID |
| `services.*.source`, `services.*.alias` | last non-empty value wins |
| `services.*.overlays`, `skills`, `workflows` | whole list is replaced when present in a later scope |
| `curation.toolSets.*.allow`, `.deny` | appended with de-duplication |
| `agents.defaultProfile` | last non-empty value wins |
| `agents.profiles.*` | merged per profile field |
| `policy.deny`, `policy.approvalRequired` | appended with de-duplication |
| `policy.allowExecSecrets` | last explicit boolean wins |
| `secrets` | replaced per secret ID |

## `enabled` defaulting for sources

A new source with no explicit `enabled` field is treated as enabled. Later scopes can then flip it on or off.

Example pattern:

- managed scope defines a source
- user scope sets `enabled: false`
- local scope sets `enabled: true`

That exact flow is covered by repository tests.

## Managed policy nuance

When the managed scope contributes `policy.deny`, the loader copies those patterns into an internal `ManagedDeny` list in addition to the public `policy.deny` list.

That matters because the current policy engine enforces `ManagedDeny` directly at execution time.

## Base directory selection

Relative document references are resolved from the effective base directory, chosen in this order:

1. the directory containing `.cli.local.json`
2. the directory containing `.cli.json`
3. the working directory passed into the loader

## Practical layout recommendation

A good split is:

- **managed**: organization-wide hard restrictions
- **user**: personal defaults or source toggles
- **project**: shared service definitions, overlays, skills, workflows
- **local**: uncommitted secrets paths, source enable/disable flips, or local overrides

See [Modes and profiles](./modes-and-profiles) for the runtime behavior that sits on top of the merged config.
