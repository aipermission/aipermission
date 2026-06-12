# Connectors

`internal/connectors` defines the internal contract for connector-shaped
targets. SSH, Postgres, and future API/Redis integrations use the same gateway
pipeline:

```text
target + credential profile + action
  -> token permission
  -> approval policy
  -> connector execution
  -> history + audit
```

The design goal is intentionally small:

```text
Connector knows the system.
Gateway knows permission.
AI knows only actions.
```

## Backend Contract

A connector implementation must provide:

- stable `Kind`, `Label`, and `Version` metadata
- non-secret target schemas and credential profile schemas for UI/API
  validation
- `GetHelp` content for MCP clients and operator guidance
- `GetActionList` action metadata
- `PrepareAction` validation and command/query/request shaping
- `ExecuteAction` execution after the gateway has allowed the action

The gateway core remains responsible for:

- API token authentication
- target/profile/action permission checks
- Prompt/Always/Disabled policy handling
- approval requests and stale-context checks
- redaction, history, and audit
- local-only HTTP/MCP boundaries

Connectors must not write audit or history rows directly. Return structured
results and let the shared action service persist them.

Target and action input schemas must not declare secret fields. Store secret
values in credential profiles and resolve them through `RuntimeContext.Secrets`
only after permission and approval checks pass.

Credential schema fields that use `secret` or `multiline_secret` must also set
`secret: true`. The registry rejects ambiguous schemas, and runtime validation
still treats those field types as secret so contributor mistakes cannot leak
credential material into public profile metadata.

Approval-required requests hash the connector kind/version, action definition,
target/profile public metadata, profile revision, encrypted secret revision,
permission rule, token validity, and prepared payload. If any of that context
changes before Run, the shared approval layer marks the request `stale`.
Connector packages should keep action definitions stable and intentionally
versioned.

Actions that return `running` need a runtime adapter owned by the gateway API
layer. The adapter is responsible for polling/finalizing the action and syncing
history. Connector packages should not add their own request lifecycle tables.

SSH is the built-in compatibility adapter, not the template for new
connectors. It uses gateway-owned runtime services for persistent PTY sessions,
SFTP/file transfer, host-key approval, generated/imported gateway keys, and
remote authorized_keys cleanup. New connectors such as Redis or HTTP API
connectors should follow the Postgres-style target/profile/action path unless a
new adapter capability is deliberately designed for every connector.

SSH runtime tables and API payloads may still say `server_id`. In the connector
model that is an SSH runtime/profile id used by the compatibility adapter, not a
generic target id. New connectors should not create `server_id`-style mirrors;
they should use connector action requests and unified history.

## Frontend Templates

Every connector kind with UI support must register templates under:

```text
frontend/src/connectors/templates/<kind>/
```

Expected files are:

- `metadata.json`: label, summary, icon, version, and badge tone
- `model.js`: display helpers such as target name, subtitle, profile label, and
  whether the target uses a live terminal
- `form.jsx`: add/edit connector target form
- `credential-form.jsx`: credential profile form
- `list-item.jsx`: connector-specific row operations
- `console.jsx`: connector console/activity surface and toolbar actions

The page-level UI should render through the template registry instead of adding
connector-specific branches to route components.

## Built-In Connector Shape

Built-in connectors may depend on runtime packages for their own transport.
SSH uses SSH key, persistent terminal, and SFTP primitives. Postgres uses
database connection and query primitives. Those transport details stay inside
the connector implementation; page-level UI, MCP tools, token permissions,
history, and audit use the shared target/profile/action vocabulary.

`RuntimeContext.Services` is reserved for gateway-owned runtime adapters. A
connector receives only services for its own kind. Do not use it as a general
escape hatch for arbitrary gateway internals.

## Adding A Connector

For a new connector such as Redis:

1. Add `internal/connectors/redis` with a small implementation of the connector
   contract.
2. Register it in the backend connector registry.
3. Store target/profile data through `internal/connectortargets`; do not create
   connector-specific permission tables.
4. Add frontend templates under `frontend/src/connectors/templates/redis`.
5. Register the templates in `frontend/src/connectors/templates/registry.jsx`
   and metadata in `catalog.js`.
6. Add MCP/help docs for its actions.
7. Add focused tests for validation, permission checks, approval-required flow,
   stale-context drift, history/audit persistence, and structured history
   search.
