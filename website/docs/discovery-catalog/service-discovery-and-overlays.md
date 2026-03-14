---
title: Service Discovery and Overlays
---

# Service Discovery and Overlays

A `serviceRoot` source discovers two things from an HTTP entrypoint:

- the OpenAPI description (`service-desc`)
- optional service metadata (`service-meta`)

## Service root discovery flow

Given:

```json
{
  "sources": {
    "ticketsService": {
      "type": "serviceRoot",
      "uri": "https://api.example.com/service",
      "enabled": true
    }
  }
}
```

The runtime:

1. tries `HEAD https://api.example.com/service`
2. falls back to `GET` if needed
3. parses the `Link` header
4. resolves relative links against the service root URL

Example header:

```http
Link: <https://api.example.com/openapi.json>; rel="service-desc", <https://api.example.com/metadata/linkset.json>; rel="service-meta"
```

## Metadata linkset format

The service metadata document is loaded as JSON and must expose a `linkset` array.

```json
{
  "linkset": [
    {
      "href": "./skills/tickets.skill.json",
      "rel": "https://oascli.dev/rel/skill-manifest"
    },
    {
      "href": "./workflows/tickets.arazzo.yaml",
      "rel": "https://oascli.dev/rel/workflows"
    },
    {
      "href": "./overlays/tickets.overlay.yaml",
      "rel": "https://oascli.dev/rel/schema-overlay"
    }
  ]
}
```

Implementation nuance: the loader looks for relation values containing the substrings:

- `skill-manifest`
- `workflows`
- `schema-overlay`

Using relation URIs that include those fragments is the safest choice.

## Overlay support

Overlays are loaded before the OpenAPI document is normalized.

Current overlay document shape:

```yaml
overlay: 1.1.0
actions:
  - target: "$.paths['/tickets'].get"
    update:
      x-cli-name: list
  - target: "$.paths['/tickets'].get.parameters[?(@.name=='status')]"
    update:
      x-cli-name: state
  - target: "$.paths['/tickets'].delete"
    remove: true
```

Supported action forms:

- `update`: merge keys into matched objects
- `remove`: delete matched fields/items
- `copy`: copy one matched value to a destination object field path

## JSONPath subset

The overlay engine supports a focused subset of JSONPath:

- object fields: `$.paths`
- quoted object fields: `$.paths['/tickets']`
- array indices: `parameters[0]`
- equality filters on arrays of objects: `[?(@.name=='status')]`

Current limitations worth documenting:

- copy destinations only support object field paths
- filter syntax is limited to `@.<field> == 'value'`
- overlay inheritance via `extends` is not implemented by the loader

## Relative reference resolution

- config-listed overlays resolve relative to the config base directory
- metadata-listed overlays resolve relative to the metadata document URL

That same rule applies to metadata-listed skills and workflows.
