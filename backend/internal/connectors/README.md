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

`GetHelp`, `GetActionList`, and `PrepareAction` must stay side-effect-free.
`GetActionList` should be static for a target/profile and must not depend on
network reachability because permission reads and MCP discovery call it on read
paths.

The gateway core remains responsible for:

- API token authentication
- target/profile/action permission checks
- `approval_required`, `always_run`, and `blocked` policy handling
- approval requests and stale-context checks
- redaction, history, and audit
- local-only HTTP/MCP boundaries

Connectors must not write audit or history rows directly. Return structured
results and let the shared action service persist them.

Target and action input schemas must not declare secret fields. Store secret
values in credential profiles and resolve them through `RuntimeContext.Secrets`
only after permission and approval checks pass.

Action inputs are persisted and returned to the UI/MCP as redacted display
payloads. The gateway keeps the raw execution payload only in the encrypted
connector action payload. Do not rely on action input JSON for secrets or
post-approval execution material; put secrets in credential profiles and let
`PrepareAction` build the raw payload that `ExecuteAction` needs.

Credential schema fields that use `secret` or `multiline_secret` must also set
`secret: true`. The registry rejects ambiguous schemas, and runtime validation
still treats those field types as secret so contributor mistakes cannot leak
credential material into public profile metadata.

Credential schema fields marked as secret must not declare defaults. Defaults
are returned to UI and catalog callers as schema metadata, so secret defaults
would leak credential material before the profile is ever saved.

Use `OutputHint.SensitiveFields` for structured response fields that should be
masked even when their names are connector-specific. The gateway also masks a
small default set such as `password`, `token`, `secret`, `api_key`, and
`authorization`, but connector-specific names must be explicit.

Approval-required requests hash the connector kind/version, action definition,
target/profile public metadata, profile revision, encrypted secret revision,
permission rule, token validity, and prepared payload. If any of that context
changes before Run, the shared approval layer marks the request `stale`.
Connector packages should keep action definitions stable and intentionally
versioned.

`PrepareAction` must be deterministic for the same request. Connector tests
should call `connectortest.AssertPrepareActionDeterministic` so approval
context hashes cannot drift because of timestamps, random defaults, map
iteration order, or hidden runtime state.

Target/profile deletion is archival from the connector action point of view.
Archived targets and profiles are hidden from future permission checks and
action execution, but existing action requests remain readable so history and
audit can prove what happened. Late async finishers must not overwrite terminal
states such as `stale`, `canceled`, or `completed`.

Actions that return `running` need a runtime adapter owned by the gateway API
layer. The adapter is responsible for polling/finalizing the action, redacting
intermediate responses, syncing history, and providing MCP assistant hints.
Connector packages should not add their own request lifecycle tables.

Connector-specific gateway capabilities live behind adapter contracts in
`internal/api/connector_api_adapters.go`. SSH uses those contracts for
persistent PTY sessions, SFTP/file transfer, host-key approval,
generated/imported gateway keys, and remote authorized_keys cleanup. The generic
HTTP handlers should ask the adapter what the connector supports; they should
not branch on `kind == "ssh"` or `kind == "postgres"`.

New connectors such as Redis or HTTP API connectors should follow the
target/profile/action path by default. If they need a capability beyond the
shared action runner, design a reusable adapter contract first instead of
adding connector-specific command tables, file-transfer tables, draft-test route
branches, or operation branches to generic handlers.

Live-console runtime payloads expose a field named `server_id` because existing
console routes use that payload name. In the connector model that value is a
connector-profile runtime id supplied by the live-console adapter, not a
generic target id and not an invitation to create connector-specific mirrors.

The Postgres connector is a read-oriented MVP. It uses read-only transactions,
statement timeouts, row caps, and output byte caps, but it is not a replacement
for database roles. Operators should use dedicated read-only Postgres users for
AI profiles.

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

The 0.2 connector line is a clean database baseline. Do not add runtime
fallbacks for pre-0.2 preview schemas; if a real user needs old data, handle it
with a separate one-time import tool.

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
