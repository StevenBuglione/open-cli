---
title: Tool Execution
---

# Tool Execution

Dynamic tool commands are generated from normalized catalog entries, then executed through the runtime's `POST /v1/tools/execute` endpoint.

## How arguments map to HTTP requests

### Path parameters

OpenAPI `in: path` parameters become positional arguments:

```bash
ocli helpdesk tickets get-ticket T-100
```

If a tool path is `/tickets/{id}`, `T-100` fills `{id}`.

### Query, header, and cookie parameters

Non-path parameters become string flags:

- query params become `--flag value`
- header params become `--flag value`
- cookie params become `--flag value`

The runtime preserves the original parameter location when constructing the upstream request.

If a parameter has `x-cli-name`, that override becomes the flag name. Otherwise `ocli` slugifies the original parameter name.

### Request bodies

The CLI supports three body forms:

```bash
# inline JSON
./bin/ocli ... create --body '{"title":"Printer jam"}'

# from a file
./bin/ocli ... create --body @ticket.json

# from stdin
cat ticket.json | ./bin/ocli ... create --body -
```

## Example

Given a tool with:

- command path `helpdesk tickets create`
- one required JSON body
- one bearer auth requirement

an execution command can look like this:

```bash
HELPDESK_TOKEN=token-abc ./bin/ocli   --runtime http://127.0.0.1:8765   --config ./.cli.json   --approval   helpdesk tickets create   --body @ticket.json
```

## What the runtime does during execution

For each tool invocation, the runtime:

1. reloads or rebuilds the catalog as needed
2. finds the tool by ID
3. evaluates policy and approval requirements
4. resolves auth secrets
5. renders the URL, query string, headers, cookies, and body
6. executes the upstream HTTP request
7. retries on `429` and `503` up to three total attempts
8. writes an audit event

## Output behavior

When the runtime returns a response:

- valid JSON bodies are returned as JSON data
- non-JSON bodies are returned as text
- `ocli --format json` prints the JSON body directly when possible
- other formats serialize the execute response wrapper

## Important implementation nuances

### Content type is always `application/json` when a body is present

The current executor sets `Content-Type: application/json` for any non-empty request body. Even if a tool schema advertises another media type, the stock CLI/runtime path is JSON-oriented today.

### Flag values are strings

All generated flags are string flags. Type-aware coercion is not implemented in the CLI layer.

### Missing path arguments are not partially validated by the executor

Cobra enforces the positional argument count for generated commands. If you call the HTTP runtime API directly, path templating only substitutes the arguments you provide.

### Retry behavior is intentionally simple

Retries happen only for `429 Too Many Requests` and `503 Service Unavailable`, with a fixed short delay between attempts. There is no exponential backoff in the current implementation.

## Approval flow

Some tools require `--approval` because of:

- OpenAPI/overlay safety metadata (`x-cli-safety.requiresApproval`)
- config policy patterns under `policy.approvalRequired`

Without approval, the runtime rejects the request before the upstream API is called.

See [Policy and approval](../security/policy-and-approval) for the exact decision order.
