# ADR 0002: No Cloud Mode

Status: accepted

## Context

Some users may ask for hosted SaaS, remote gateway hosting, or cloud-managed
execution. Those requests are understandable, but they change the product's
security and operational responsibilities.

## Decision

AIPermission will not implement hosted SaaS mode, cloud-managed execution, or
remote gateway hosting in the core project.

## Consequences

- The core project remains local-only and single-user.
- The project does not manage accounts, organizations, tenants, billing, or
  hosted execution infrastructure.
- Cloud mode proposals conflict with the project principles and should normally
  be closed as `wontfix`.

## Related

- [Project Principles](../project-principles.md)
- [ADR 0001: Local-Only Gateway](0001-local-only.md)
