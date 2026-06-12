# Contributing

Thanks for wanting to improve `aipermission`.

The project is in active MVP testing. The current focus is a reliable local developer workflow:

- Docker Compose local runtime
- safe connector credential handling
- MCP connector action execution
- approval flow
- persistent SSH console visibility
- clear documentation

Before proposing scope changes, read [Project Principles](docs/project-principles.md).
AIPermission is local-only, single-user, developer-focused, connector-based,
and human-in-the-loop. Hosted SaaS, team RBAC, remote gateway hosting,
LAN-accessible deployments, and cloud-managed execution are intentionally out
of scope for the core project.

## Development

Install JavaScript workspaces from the repository root:

```bash
npm install
```

Run backend tests:

```bash
cd backend
go test ./...
```

Build frontend:

```bash
npm run build --workspace frontend
```

Build MCP bridge:

```bash
npm run build --workspace @aipermission/mcp
```

If your AI client runs from the monorepo root, use the workspace MCP command in
[MCP Client Setup](docs/setup/mcp-client-setup.md#local-package-development)
instead of the normal `npx -y @aipermission/mcp` command.

When adding a new target type, start with [Add A Connector](docs/development/add-a-connector.md).
New connectors must use the shared target/profile/action permission pipeline
instead of adding connector-specific approval, history, audit, or MCP tool
families.

New connector PR checklist:

- add `backend/internal/connectors/<kind>/` and register it in the built-in
  connector registry
- add frontend templates under `frontend/src/connectors/templates/<kind>/` and
  register them in the template registry/catalog
- keep secrets in credential profile schemas, not target or action schemas
- use the shared target/profile/action permission model; do not add connector
  permission, approval, history, audit, or MCP tool families
- document any intentional runtime adapter exception before using
  `RuntimeContext.Services`
- update smoke/tests that assert the built-in connector list and template
  registrations

Run the full local stack:

```bash
docker compose up -d --build
```

## Pull Requests

Before opening a PR:

- keep changes focused
- update docs when behavior changes
- avoid logging or returning credentials
- run the relevant tests/builds
- describe manual testing for MCP, approvals, or console changes

## Security Boundaries

Do not add code that exposes:

- SSH private keys
- gateway vault secret
- database passwords
- backup files
- raw credentials in logs, API responses, MCP responses, or audit payloads

When in doubt, open an issue first.
