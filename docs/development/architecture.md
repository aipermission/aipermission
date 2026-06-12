# Development Architecture

AIPermission is a monorepo because the gateway, frontend, MCP bridge, operator instructions, and docs share one security contract.

```txt
aipermission/
  backend/                  Go local gateway, connector runtime, and MCP API
  frontend/                 React web UI served by local nginx
  packages/mcp/             npm MCP bridge package
  packages/npm-placeholder/ unscoped npm placeholder package
  docs/                     product, API, security, and setup docs
```

## Runtime Shape

```txt
browser -> localhost frontend/nginx -> backend API -> encrypted SQLite + connector targets
MCP client -> localhost frontend/nginx -> backend /api/mcp/* -> connector targets
```

The backend is not a LAN service. Docker Compose publishes the UI on `127.0.0.1`, the backend binds to loopback, and nginx proxies `/api` internally.

Connector targets use one shared security pipeline:

```txt
target + credential profile + action
  -> token permission
  -> approval policy
  -> connector execution
  -> history + audit
```

SSH and Postgres are built-in connectors that share the same target/profile,
permission, approval, history, and audit model. SSH owns a live terminal and
file-transfer surface; Postgres owns structured database actions. Future
connectors should add their own execution surface without adding a new
permission or audit pipeline.

## Backend Boundaries

- `internal/api`: HTTP routes, MCP handlers, UI session/CSRF, approval/message flows, and workspace lifecycle orchestration.
- `internal/connectors`: connector contracts and built-in connector
  implementations. Connector packages describe target schemas, credential
  schemas, help/actions, validation, and execution. They do not own
  permissions, audit, or history.
- `internal/connectortargets`: connector target/profile/action storage plus the
  shared action request model.
- `internal/actions`: shared action execution service used by structured
  connectors after permission checks.
- `internal/history`: unified history projection for command, action, and file
  transfer activity.
- `internal/console`: persistent SSH console sessions, PTY websocket attach, AI command execution inside a shell session, transcript display cleanup, and transcript redaction before persistence. Console persistence uses a bounded session snapshot plus append-only transcript chunks so long-running sessions do not rewrite one large transcript row on every flush.
- `internal/db`: SQLCipher open, schema migrations, database catalog, encrypted database lifecycle.
- `internal/tokens`: API token create/hash/revoke/permission storage.
- `internal/sshkeys`: gateway-owned SSH key generation, explicit private key import, and vault-backed private key storage used by the SSH connector.
- `internal/sshconfig`: conservative SSH config host discovery/parsing for SSH connector form prefill.
- `internal/execution`: SSH command execution, SFTP file transfer primitives,
  and host key verification.
- `internal/filetransfer`: file transfer history metadata, progress, status, and
  checksum storage. File contents are not stored in SQLCipher.
- `internal/vault`: AES-GCM secret payload encryption inside the SQLCipher database.

Large API files should be split by behavior before they become cross-domain modules. Runtime-heavy domains should move out of `internal/api` when possible; `internal/console` is the first example of that boundary. Prefer small handler/service files such as `mcp_auth.go`, `mcp_command_requests.go`, and `ssh_host_key_handlers.go`. Route handlers should usually hang off small handler groups (`mcpHandlers`, `tokenHandlers`, `consoleHandlers`) instead of adding every endpoint directly to `*Server`.

## Frontend Boundaries

- `src/pages`: route-level pages.
- `src/components`: reusable UI and domain components.
- `src/lib`: API client, gateway context, hooks, and shared helpers.
- `src/connectors/templates`: connector UI templates. Each connector kind owns
  its form, credential form, row actions, console surface, toolbar actions, and
  display model.

Keep token connector-action permission logic in shared hooks such as
`useConnectorPermissions` instead of duplicating it between Console and Tokens
pages.

Route-level pages should render connector-specific UI through
`src/connectors/templates/registry.jsx`. Avoid adding new `if kind === "..."`
branches to pages when the behavior belongs to a connector template.

## MCP Package

`packages/mcp` is published as `@aipermission/mcp`. It should stay small:

- CLI entrypoint
- provider config installer
- MCP stdio server
- bundled operator instruction resource
- tests for config writing and skill installation

The unscoped `aipermission` npm package is only a placeholder that points users to `@aipermission/mcp`.
