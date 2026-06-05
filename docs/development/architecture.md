# Development Architecture

AIPermission is a monorepo because the gateway, frontend, MCP bridge, operator instructions, and docs share one security contract.

```txt
aipermission/
  backend/                  Go local gateway and SSH/MCP execution API
  frontend/                 React web UI served by local nginx
  packages/mcp/             npm MCP bridge package
  packages/npm-placeholder/ unscoped npm placeholder package
  docs/                     product, API, security, and setup docs
```

## Runtime Shape

```txt
browser -> localhost frontend/nginx -> backend API -> encrypted SQLite + SSH targets
MCP client -> localhost frontend/nginx -> backend /api/mcp/* -> SSH targets
```

The backend is not a LAN service. Docker Compose publishes the UI on `127.0.0.1`, the backend binds to loopback, and nginx proxies `/api` internally.

## Backend Boundaries

- `internal/api`: HTTP routes, MCP handlers, UI session/CSRF, approval/message flows, and workspace lifecycle orchestration.
- `internal/console`: persistent SSH console sessions, PTY websocket attach, AI command execution inside a shell session, transcript display cleanup, and transcript redaction before persistence. Console persistence uses a bounded session snapshot plus append-only transcript chunks so long-running sessions do not rewrite one large transcript row on every flush.
- `internal/db`: SQLCipher open, schema migrations, database catalog, encrypted database lifecycle.
- `internal/tokens`: API token create/hash/revoke/permission storage.
- `internal/sshkeys`: gateway-owned SSH key generation, explicit private key import, and vault-backed private key storage.
- `internal/sshconfig`: conservative SSH config host discovery/parsing for server form prefill.
- `internal/servers`: SSH target records.
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

Keep token permission logic in shared hooks such as `useTokenPermissions` instead of duplicating it between Console and Tokens pages.

## MCP Package

`packages/mcp` is published as `@aipermission/mcp`. It should stay small:

- CLI entrypoint
- provider config installer
- MCP stdio server
- bundled operator instruction resource
- tests for config writing and skill installation

The unscoped `aipermission` npm package is only a placeholder that points users to `@aipermission/mcp`.
