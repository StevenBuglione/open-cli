# OAS-CLI CLI Profile 0.1.0

## Command Shape

The CLI command shape is:

```text
oascli <service> <group> <command> [args...] [flags...]
```

## Group Derivation

Implementations derive groups in this order:

1. `x-cli-group`
2. first OpenAPI tag
3. first non-parameter path segment
4. `misc`

## Command Derivation

Implementations derive command names in this order:

1. `x-cli-name`
2. normalized `operationId`
3. inferred verb from HTTP method plus path

## Parameters

- path parameters are positional arguments in path order
- query, header, and cookie parameters become flags
- request bodies must support structured JSON input

## Output

Implementations MUST support:

- `json`
- `yaml`
- `pretty`

They MUST also expose a machine-readable tool schema derived from the normalized catalog.
