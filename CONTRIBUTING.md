# Contributing

Thanks for wanting to improve `aipermission`.

The project is in active MVP testing. The current focus is a reliable local developer workflow:

- Docker Compose local runtime
- safe SSH key handling
- MCP command execution
- approval flow
- persistent console visibility
- clear documentation

Before proposing scope changes, read [Project Principles](docs/project-principles.md).
AIPermission is local-only, single-user, developer-focused, SSH-based, and
human-in-the-loop. Hosted SaaS, team RBAC, remote gateway hosting,
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
