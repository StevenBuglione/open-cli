---
title: Skill Manifests
---

# Skill Manifests

Skill manifests add user-facing guidance to normalized tools.

## File format

The current loader expects JSON with a `toolGuidance` object keyed by tool ID.

```json
{
  "oasCliSkill": "1.0.0",
  "serviceId": "tickets",
  "summary": "Guidance for using the Helpdesk API via Open CLI",
  "toolGuidance": {
    "tickets:listTickets": {
      "whenToUse": [
        "You need the current queue of tickets"
      ],
      "avoidWhen": [
        "You already know the ticket ID and should fetch it directly"
      ],
      "examples": [
        {
          "goal": "List only open tickets",
          "command": "ocli helpdesk tickets list-tickets --status open"
        }
      ]
    }
  }
}
```

Only `toolGuidance` is consumed by the current loader. Extra top-level metadata is harmless documentation for humans, but it does not drive runtime behavior.

## Guidance fields

Each tool entry can contain:

- `whenToUse`: list of positive cues
- `avoidWhen`: list of anti-patterns
- `examples`: list of `{goal, command}` objects

## Merge behavior

If multiple skill manifests define the same tool ID, later documents overwrite earlier guidance for that tool.

That can happen because the builder loads manifests in config order and also merges in metadata-discovered refs.

## Where guidance shows up

- `tool schema` includes the guidance under `guidance`
- `explain` returns a compact guidance-centered view
- dynamic command help text does **not** currently render skill guidance inline

## Authoring tips

- key entries by normalized tool ID (`serviceId:operationId`)
- use command examples that match the actual generated command path, including aliases and renamed flags
- keep examples aligned with real config fields such as `--approval`, `--agent-profile`, and `--format`

## Relative paths

- config-listed skill manifests resolve relative to the config base directory
- metadata-listed skill manifests resolve relative to the service metadata URL
