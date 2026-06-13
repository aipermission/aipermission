# Add A Connector

AIPermission connectors all use the same product pipeline:

```txt
target + credential profile + action
  -> token permission
  -> approval policy
  -> connector execution
  -> history + audit
```

The connector owns transport-specific behavior. The gateway owns permission,
approval, history, audit, local-only HTTP/MCP boundaries, and token checks.

## Connector Invariants

These rules are part of the connector contract:

- A connector does not create its own token permission, approval, history,
  audit, or MCP tool pipeline.
- A connector target stores non-secret connection metadata. A credential
  profile stores public identity metadata plus encrypted secret material.
- Target schemas must not declare secret fields. The backend rejects secret
  target fields so credentials cannot drift into non-secret target metadata.
- Credential schema fields that use `secret` or `multiline_secret` types must
  also set `secret: true`. The registry rejects malformed credential schemas,
  and runtime validation treats those field types as secret even if a
  contributor forgot the flag.
- Secret credential fields must not declare defaults. Schema metadata is
  readable by the local UI/API, so defaults are for non-secret UI hints only.
- Connector-specific structured output secrets must be listed in
  `OutputHint.SensitiveFields` so the shared redaction layer masks them in MCP
  responses, history, and audit.
- Action input JSON is persisted and returned as a redacted display payload.
  The raw execution payload is kept only in the encrypted connector action
  payload. Never put API tokens, passwords, private keys, or tenant secrets in
  action input schemas; define them as credential profile fields instead.
- `GetHelp`, `GetActionList`, and `PrepareAction` are side-effect-free and must
  not read raw secrets.
- `ExecuteAction` is the only connector method that receives raw secrets, and
  only after the gateway has allowed the action.
- Action input schemas must not contain secret fields. Put passwords, API keys,
  tokens, private keys, tenant ids that must remain secret, and similar material
  in credential profile schemas.
- Non-SSH connector actions are synchronous in 0.2.x. If a connector needs
  `running`/polling semantics, add a reusable gateway runtime adapter contract
  first; do not add connector-local polling tables.
- New connectors must not add `server_id`, SSH-style command tables,
  file-transfer tables, draft-test route branches, or operation routes unless a
  reusable adapter contract has been reviewed. Use connector action requests and
  unified history by default.
- Route pages render through frontend templates. Do not add `if kind ===
  "redis"` branches to generic pages.

SSH is the built-in compatibility adapter, not the template for new connectors.
It has extra gateway-owned behavior for persistent PTY sessions, SFTP transfer,
host-key approval, key generation/import, and remote authorized_keys cleanup.
New connectors should follow the generic Postgres-style target/profile/action
path unless they have an explicitly reviewed core adapter reason.

Some SSH runtime surfaces still expose `server_id` because the live terminal,
SSH command request rows, and file-transfer queues need a stable runtime id.
For 0.2.x this id is an SSH adapter/profile-backed runtime id, not the generic
connector target id. New connectors must not add their own `server_id` model or
copy SSH command/file-transfer tables. They should use connector action
requests and unified history unless a reusable gateway runtime adapter has been
designed first.

The `Credentials` page stores connector credential profiles. The older SSH key
material is now one kind of connector credential profile, not the model for
every connector. For API or Redis-style connectors, add the profile fields that
the connector needs and keep secret values in encrypted credential schemas.

For 0.2.x, SSH targets intentionally support one credential profile in the
compatibility adapter. That avoids hidden profile selection in persistent
console, SFTP transfer, authorized_keys cleanup, and MCP compatibility paths.
New connectors should support multiple profiles through the generic
target/profile/action model unless their adapter contract explicitly says
otherwise.

## Backend Contract

Add a backend package under:

```txt
backend/internal/connectors/<kind>/
```

A connector implementation must provide:

- stable `Kind`, `Label`, and `Version` metadata
- target schema fields for connection settings
- credential profile schema fields for secrets and identity
- `GetHelp` content for MCP/operator guidance
- `GetActionList` action metadata for one target/profile
- `PrepareAction` validation and normalized action input
- `ExecuteAction` transport-specific execution

Connectors must not create their own permission, approval, history, or audit
pipeline. They return structured results; the shared gateway services persist
request state, output, errors, and audit records.

Target schemas must be non-secret. Use target schemas for endpoint metadata
such as host, port, database name, or API base URL. Use credential schemas for
passwords, tokens, private keys, tenant secrets, and anything that should be
vault-encrypted. If a credential schema uses a secret field type, mark that
field with `secret: true`; ambiguous secret fields fail registry validation.
Do not put defaults on secret credential fields.

The Connectors page manages a target and its default credential profile.
Additional profiles for the same target belong on the Credentials page. Token
permissions always bind one target, one credential profile, and one action.

Draft target tests before save are currently SSH-only because SSH host-key
approval and remote key installation happen before the local target/profile is
persisted. Normal structured connectors should implement saved profile tests
through `TestableConnector`; do not add connector-specific draft-test branches
to generic route handlers without designing a reusable contract first.

Action input schemas must not contain secret fields. Put passwords, API keys,
tokens, private keys, and tenant-specific secret material in credential profile
schemas so the gateway can encrypt and redact them consistently. `PrepareAction`
may validate references to credential profile metadata, but raw secrets are
available only through `RuntimeContext.Secrets` during approved execution.
For connector-specific output fields that contain sensitive material, set
`ActionDefinition.OutputHint.SensitiveFields`. The gateway masks those field
names in structured output before returning MCP responses or persisting
history/audit payloads.

The same redaction rule applies to action inputs and approval displays. The
gateway persists redacted input JSON, while the raw prepared execution payload
is encrypted separately for the action runner. Tests for a connector should
prove that realistic secret-looking input/output values are masked in MCP
responses and history.

Approval-required actions store a context snapshot when the request is created.
That snapshot includes token validity, permission rule, target/profile public
metadata, profile revision, encrypted secret revision, connector kind/version,
action definition hash, and action payload hash. If those values drift before
the operator clicks Run, the request becomes `stale` and the AI must submit a
fresh action request.

Synchronous connector actions can return `completed`, `failed`, or `error`
directly from `ExecuteAction`. Long-running `running` actions require an
explicit runtime adapter in `internal/api` so polling, output finalization,
redaction, history sync, and MCP assistant hints stay centralized. Do not
invent connector-local polling tables.

## Frontend Templates

Add templates and register them under:

```txt
frontend/src/connectors/templates/<kind>/
```

The folder alone is not enough. Static registration is required in
`registry.jsx` and `catalog.js` so the Vite bundle and route-level pages know
which template module to render.

Expected files:

- `metadata.json`: label, summary, icon, version, and badge tone
- `model.js`: display helpers, target subtitle, profile labels, operations, and
  whether the target uses a live terminal
- `form.jsx`: add/edit connector target form
- `credential-form.jsx`: credential profile form
- `list-item.jsx`: connector-specific row operations
- `console.jsx`: connector console/activity surface and toolbar actions

Template slots:

| File or export | Required | Use it for |
|---|---:|---|
| `metadata.json` | yes | Connector label, version, summary, icon, and badge tone. |
| `model.js` | yes | Display helpers, target/profile labels, endpoint text, test/delete behavior, and whether the target uses a live terminal. |
| `form.jsx` | yes | Add/edit target fields for the connector target schema. |
| `credential-form.jsx` | yes | Add/edit credential profile fields for the credential schema. |
| `list-item.jsx` | yes | Connector-specific row operations on the Connectors page. Do not put generic Edit/Delete/Test actions here. |
| `console.jsx` | yes | Structured activity surface or live-console wrapper for the Console page. |
| `CredentialRowActions` | optional | Extra credential-row actions, such as copying an SSH install command. |
| `ToolbarActions` | optional | Connector-specific Console toolbar actions, such as Files or Bulk for SSH. |
| `Operations` | optional | Connector-specific dialogs/operations launched from list rows. |

`model.js` is the connector UI contract. Keep these exports small and
connector-local:

| Export | Required | Purpose |
|---|---:|---|
| `emptyForm` | yes | Initial add-target form state. |
| `formFromTarget` | yes | Convert saved target/profile data into edit form state. |
| `save` | yes | Create or update the target plus default profile. Use shared target/profile helpers where possible. |
| `deleteTarget` | yes | Invoke generic target delete/archive, plus connector-specific cleanup options when needed. |
| `test` | yes | Run the saved target/profile connection test. |
| `targetDisplayName` / `targetSubtitle` / `targetEndpoint` | yes | Labels used by generic target lists and console headers. |
| `targetProfileLabel` | yes | Profile label shown in the Console token panel. |
| `usesLiveConsole` | yes | `true` only for adapters that own a live terminal runtime. |
| `deleteDialog` | yes | Copy and action buttons for target deletion. |
| `emptyCredentialState`, `credentialStateFromRow`, `credentialFormProps`, `saveCredential`, `deleteCredential`, `credentialRows` | yes | Generic Credentials page integration. |
| `credentialHint`, `canEdit`, `canDelete` | yes | Generic row affordances. |
| `hostKeyActionFromError`, `resumeHostKeyAction` | yes | Return `null` / throw for non-SSH connectors; SSH uses this for host-key approval retry. |

Connector templates may add optional exports for connector-owned operations,
but generic Test/Edit/Delete and permission/history behavior must stay in the
shared pages and stores.

Register the connector in:

```txt
frontend/src/connectors/templates/registry.jsx
frontend/src/connectors/templates/catalog.js
backend/internal/connectors/builtin/registry.go
```

Static registration is intentional for Vite and the Go binary. Adding a
connector should require registration files and tests, but it should not require
new generic route handlers, permission tables, history tables, audit tables, or
MCP tool families.

Route-level pages should render through the template registry. Avoid adding
new `if kind === "redis"` branches to generic pages.

Schema defaults are declarative UI hints and validation aids. Connector code
must still normalize defaults in `PrepareAction` or `ExecuteAction` before
building payloads, opening sockets, or running transport-specific logic.

## Example: Redis

A Redis connector should add only Redis-specific behavior:

- target fields such as host, port, TLS mode, and database index
- credential profile fields such as username/password or token
- actions such as `get_info`, `scan_keys`, or `get_key`
- connector help that explains safe key inspection
- UI templates for Redis target rows, credential profiles, and activity output

It should not add a Redis-specific token permission table, approval table,
history page, audit route, MCP tool family, or global UI page.

Minimal Redis skeleton checklist:

- `backend/internal/connectors/redis/redis.go`
- `backend/internal/connectors/redis/redis_test.go`
- backend registry entry and registry test
- `frontend/src/connectors/templates/redis/metadata.json`
- `frontend/src/connectors/templates/redis/model.js`
- `frontend/src/connectors/templates/redis/form.jsx`
- `frontend/src/connectors/templates/redis/credential-form.jsx`
- `frontend/src/connectors/templates/redis/list-item.jsx`
- `frontend/src/connectors/templates/redis/console.jsx`
- frontend registry/catalog entries and smoke tests
- README, REST/MCP docs, and connector-specific safety notes

## Postgres Safety Boundary

The built-in Postgres connector is intentionally conservative. `query_readonly`
rejects obvious write statements, enforces a SQL size limit, executes with a
read-only transaction, applies a statement timeout, caps row count, and caps
returned output bytes before MCP/history persistence. Those controls are not a
substitute for database-level least privilege. Use dedicated read-only roles for
AI profiles and prefer `approval_required` for exploratory SQL.

## Tests

Add focused tests for:

- target schema validation
- credential profile validation
- action list visibility for a real target/profile
- target schema rejection for secret fields
- action input schema rejection for secret fields
- secret credential schema rejection when defaults are present
- connector-specific `OutputHint.SensitiveFields` redaction
- `blocked`, `approval_required`, and `always_run` permission behavior
- approval-required execution and stale-context behavior
- stale request finalization when target/profile/action context changes or is
  deleted before approval
- history/audit persistence through the shared pipeline
- structured input/output JSON search through unified history
- connector action source propagation into unified history
- frontend template registry coverage
- frontend smoke coverage for the built-in connector list, registration files,
  and route-level pages avoiding connector-specific branches

Exact checklist for built-in connector registration:

- backend implementation and focused tests under
  `backend/internal/connectors/<kind>/`
- backend registration in `backend/internal/connectors/builtin/registry.go`
- backend registry coverage in
  `backend/internal/connectors/builtin/registry_test.go`
- frontend templates under `frontend/src/connectors/templates/<kind>/`
- frontend registration in `frontend/src/connectors/templates/registry.jsx`
  and `frontend/src/connectors/templates/catalog.js`
- frontend smoke coverage in `frontend/src/lib/app.smoke.test.js`
- public docs updates for user-visible setup, REST, MCP, or security behavior

Before review, grep for the connector kind outside its implementation and
template folders. Expected references are registration, tests, and docs. New
generic pages should not gain connector-specific branches.

## Documentation

Update:

- `docs/api/mcp-tools.md` when action behavior affects MCP clients
- `docs/api/rest-api.md` when REST endpoints or payloads change
- `docs/development/architecture.md` when connector boundaries change
- connector-specific setup docs when the target needs external preparation
