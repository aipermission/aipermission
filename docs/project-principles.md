# Project Principles

AIPermission has a deliberately narrow product shape. These principles are
part of the security model, not marketing copy.

## AIPermission Is Intentionally

- Local-only
- Single-user
- Developer-focused
- Connector-based, with built-in SSH, Postgres, Redis, and RabbitMQ
- Human-in-the-loop

## AIPermission Intentionally Rejects

- Hosted SaaS deployments
- Multi-user RBAC systems
- Team collaboration features
- Cloud-managed execution
- LAN-accessible deployments

## Why This Boundary Exists

AIPermission gives AI agents controlled action access to real systems through
connector targets. That is powerful, so the product keeps the trust boundary
small:

- the gateway runs on the developer's own machine,
- the browser UI talks to localhost,
- MCP clients authenticate with local API tokens,
- connector credentials stay inside the encrypted local gateway,
- humans can require approval before connector actions run.

Turning the gateway into a shared service would require a different product:
remote authentication, account recovery, CSRF assumptions, tenant isolation,
RBAC, team audit policy, network hardening, and incident response workflows.
Those are intentionally outside this project's scope.

## Will Not Implement

The following requests conflict with the project principles and should normally
be closed as `wontfix`:

- Hosted SaaS mode
- Multi-tenant architecture
- Remote gateway hosting
- Shared team deployments
- LAN-accessible gateway mode
- Cloud-managed action execution

Suggested closure note:

```text
Closed as wontfix.
Conflicts with AIPermission project principles:
local-only, single-user, developer-focused, human-in-the-loop.
```

## Acceptable Extensions

These areas can evolve without changing the core identity:

- better MCP client setup,
- safer approval UX,
- clearer audit/history browsing,
- stronger local hardening,
- more tests,
- documentation and troubleshooting,
- local action policy warnings,
- temporary token or permission expiration.
- additional local connector types that preserve the same target/profile/action
  permission model.

When in doubt, ask whether the proposal keeps AIPermission a local permission
gateway for one developer. If it turns the project into a hosted operations
platform, it belongs outside the core project.
