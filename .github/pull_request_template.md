## Summary


## Alignment with project vision

- [ ] This change keeps AIPermission local-only, single-user, developer-focused, and human-in-the-loop.
- [ ] This change does not add hosted SaaS, multi-user RBAC, team collaboration, remote gateway hosting, LAN-accessible deployment, or cloud-managed execution behavior.

## Breaking change check

Does this change require updates to the security model, permission model, MCP
protocol behavior, or local-only architecture?

- [ ] No
- [ ] Yes, explained below

## Testing

- [ ] `go test ./...`
- [ ] frontend build
- [ ] MCP build
- [ ] manual Docker/MCP test, if relevant

## Security checklist

- [ ] No SSH private keys, database passwords, API tokens, or reusable token values are logged.
- [ ] REST/MCP changes preserve the class-specific [credential boundary](../docs/security/credential-boundary.md), including its narrow authenticated-local exceptions.
- [ ] New or changed persisted command/audit/message payloads pass through redaction where appropriate.
- [ ] Mutating web UI routes remain covered by the local UI session and CSRF checks.
- [ ] MCP routes still require token auth and preserve token/server permission checks.
- [ ] `always_run` behavior remains explicit and visible to the user.
- [ ] Local-only assumptions are preserved; no LAN/public gateway path is introduced.
- [ ] Docs are updated when behavior, security boundaries, or API/MCP contracts change.
