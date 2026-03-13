# OAS-CLI Config 0.1.0

## File Locations

Recommended scope locations:

- Managed: `/etc/oas-cli/.cli.json`
- User: `$XDG_CONFIG_HOME/oas-cli/.cli.json`
- Project: `<repo>/.cli.json`
- Local: `<repo>/.cli.local.json`

## Precedence

Scopes merge in this order:

1. Managed
2. User
3. Project
4. Local

Later scopes override earlier mutable values, but Managed denies remain absolute.

## Merge Rules

- keyed maps merge by key
- explicit `enabled: false` disables a source or service without deleting it
- allow and deny arrays append uniquely across scopes
- managed denies are preserved separately and remain non-overridable

## Secret References

Secret values MUST NOT be embedded directly in project config. Supported reference types are:

- `env`
- `file`
- `osKeychain`
- `exec`

`exec` resolution MUST be disabled unless explicitly enabled by policy.
