# ADR 0003: Single-User Design

Status: accepted

## Context

AIPermission has tokens and permissions, but those are for local AI clients and
server scopes. They are not a team access-control model.

## Decision

AIPermission is a single-user developer tool. It will not implement team RBAC,
multi-user web accounts, shared organizations, or collaborative deployments in
the core project.

## Consequences

- Token target/profile/action permissions scope AI agent access, not human team membership.
- The local UI is protected by local-only networking, database unlock, browser
  session, and CSRF checks.
- Team collaboration proposals should normally be closed as `wontfix`.

## Related

- [Project Principles](../project-principles.md)
- [MCP Permission Flow](../architecture/mcp-permission-flow.md)
