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

The 0.2 connector line is treated as a clean connector-schema boundary. It is
allowed to make breaking local database changes while the project is still
pre-1.0, and the implementation should avoid long-term compatibility shims that
keep SSH outside the shared connector path. Pre-0.2 preview databases are not
migrated automatically; users should create a fresh 0.2 database before testing
the connector-native line. If a real user needs to preserve important 0.1.x
data, handle it with a separate one-time import tool instead of runtime
compatibility code.

Connector work has two classes:

| Capability | Normal structured connector | Runtime-integrated connector |
|---|---|---|
| Examples | Postgres, Redis, API recipes | SSH live terminal and SFTP |
| Backend connector package | yes | yes |
| Frontend template folder | yes | yes |
| Shared target/profile/action permissions | yes | yes |
| Shared approval, history, and audit | yes | yes |
| New permission/history/audit tables | no | no |
| Generic route branches such as `kind == "redis"` | no | no |
| `connector_api_adapters.go` work | no | only after design review |
| Live console / file transfer / owned credential resources | no | adapter contract required |

If a connector cannot fit the normal structured path, treat that as a design
review signal before adding gateway-owned adapter capabilities. Adapter
capabilities must be expressed through the typed `internal/connectorapi`
interfaces so runtime-integrated connectors do not invent parallel server,
runtime, lifecycle, or credential-resource contracts.

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
- `internal/api/connector_api_adapters.go`: gateway-owned connector capability
  adapters. Generic API handlers ask these adapters whether a connector
  supports live-console runtime ids, draft tests, target operations, async
  finalization, or other gateway-owned behavior. They should not branch on a
  connector kind directly.
- `internal/history`: unified history projection for command, action, and file
  transfer activity.
- `internal/console`: persistent SSH console sessions, PTY websocket attach, AI command execution inside a shell session, transcript display cleanup, and transcript redaction before persistence. Console persistence uses a bounded session snapshot plus append-only transcript chunks so long-running sessions do not rewrite one large transcript row on every flush.
- `internal/db`: SQLCipher open, schema migrations, database catalog, encrypted database lifecycle.
- `internal/tokens`: API token create/hash/revoke/permission storage.
- `internal/connectors/ssh/sshkeys`: gateway-owned SSH key generation, explicit private key import, and vault-backed private key storage used by the SSH connector.
- `internal/connectors/ssh/sshconfig`: conservative SSH config host discovery/parsing for SSH connector form prefill.
- `internal/connectors/ssh/execution`: SSH command execution, SFTP file
  transfer primitives, and host key verification owned by the SSH connector.
- `internal/filetransfer`: file transfer history metadata, progress, status, and
  checksum storage. File contents are not stored in SQLCipher.
- `internal/vault`: AES-GCM secret payload encryption inside the SQLCipher database.

Large API files should be split by behavior before they become cross-domain modules. Runtime-heavy domains should move out of `internal/api` when possible; `internal/console` is the first example of that boundary. Prefer small handler/service files such as `mcp_auth.go`, `command_requests.go`, `command_request_queries.go`, and connector adapter files. Route handlers should usually hang off small handler groups (`mcpHandlers`, `tokenHandlers`, `consoleHandlers`) instead of adding every endpoint directly to `*Server`.

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
