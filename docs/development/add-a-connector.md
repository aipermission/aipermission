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
The generic target layer also owns project assignment and token project-scope
enforcement. A connector must not implement project-specific storage, routes,
filters, or permission checks.

## Connector Invariants

These rules are part of the connector contract:

- A connector does not create its own token permission, approval, history,
  audit, or MCP tool pipeline.
- A connector does not create its own project model. Shared target save helpers
  carry `project_id`; generic UI and backend layers group and scope targets.
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
- Generated short-lived bearer capabilities that the authorized caller must
  receive may be listed in `OutputHint.TemporaryCapabilityFields`. This rare
  exception preserves signed string syntax while retaining custom redaction;
  the value remains in encrypted history until retention removes it. Never use
  it for source credentials, refresh tokens, or long-lived secrets.
- Arbitrary action inputs that may contain sensitive content, such as message
  keys, values, or headers, must be listed in
  `ActionDefinition.SensitiveInputFields`.
- Action input JSON is persisted and returned as a redacted display payload.
  The raw execution payload is kept only in the encrypted connector action
  payload. Never put API tokens, passwords, private keys, or tenant secrets in
  action input schemas; define them as credential profile fields instead.
- Declare ports, limits, counts, offsets, TTLs, and timeouts as `integer`.
  Shared validation rejects fractions and out-of-range values before connector
  code runs. Use `number` only when decimal input is intentional.
- `GetHelp`, `GetActionList`, and `PrepareAction` are side-effect-free and must
  not read raw secrets. `GetActionList` is the permission catalog for the
  connector kind: it must stay stable across target/profile public metadata and
  must not open network connections; permission screens, approval drift checks,
  and MCP discovery call it during read paths.
- Dynamic API recipes must not create per-target action names in the 0.2
  permission model. Prefer a stable action such as `call_operation` with a
  recipe operation id in the action input. Supporting target/profile-scoped
  action catalogs would be a shared permission-model change, not a
  connector-local shortcut.
- `PrepareAction` must be deterministic for the same target/profile/action
  input. Use `connectortest.AssertPrepareActionDeterministic` in connector
  tests so approval-context hashes cannot drift because of timestamps, random
  defaults, map iteration, or hidden runtime state.
- `ExecuteAction` is the only connector method that receives raw secrets, and
  only after the gateway has allowed the action.
- Action input schemas must not contain secret fields. Put passwords, API keys,
  tokens, private keys, tenant ids that must remain secret, and similar material
  in credential profile schemas.
- Synchronous connector actions can use the shared connector action runner
  directly. If a connector needs `running`/polling semantics or another
  gateway-owned capability, add a reusable adapter contract first; do not add
  connector-local polling tables.
- New connectors must not add connector-specific command tables,
  file-transfer tables, draft-test route branches, or operation routes unless a
  reusable adapter contract has been reviewed. Use connector action requests and
  unified history by default.
- Route pages render through frontend templates. Do not add `if kind ===
"redis"` branches to generic pages.

Connector-specific gateway capabilities live behind adapter contracts in
`internal/gatewayconnectorapi` and are registered through the connector adapter
registry. SSH uses those contracts for persistent PTY sessions, SFTP transfer, host-key approval, key
generation/import, reviewed TCP transport, reviewed command transport, and
remote authorized_keys cleanup. Generic route handlers must ask the adapter
what the connector supports instead of branching on a connector kind.

Adapter methods use the smallest applicable consumer-owned ports in
`internal/gatewayconnectorapi`, such as `ConnectorDataRuntime`, `ActionRuntime`,
`TransferRuntime`, `TargetLifecycleRuntime`, `RuntimeActionGateway`,
`FileTransferGateway`, `TargetDeletionGateway`, or `TargetOperationGateway`.
Connection tests receive read-only peer identity rather than lifecycle
authority, and credential canonicalization receives no gateway service. Do not
introduce a concrete gateway escape hatch or connector-local copies of these
contracts. If a reusable capability needs a new service, add a narrow port,
compose it only where required, and update the exact-method-set architecture
tests.

Target operations run under workspace delivery admission. Persistent-state
operations must opt into `TargetOperationLifecyclePolicy`; see the canonical
[backend contract](../../backend/internal/connectors/README.md#backend-contract)
for lifecycle exclusion and snapshot ownership.

Runtime-backed capabilities expose `runtime_id` as the shared identifier for a
connector-profile capability surface. The adapter must resolve that id and
fail closed unless connector kind, target/profile identity, and capability kind
match its own contract. SSH and S3 both use the generic file-transfer adapter;
paginated browse and recursive selection are optional typed extensions. New
connectors must not add their own runtime-id model or copy command/file-transfer
tables unless a reusable gateway runtime adapter has been designed first.

The `Credentials` page manages connector credential profiles and
connector-owned credential resources. SSH key material is a resource used by
SSH profiles, not a generic model for every connector. For API or Redis-style
connectors, add only the profile/resource fields that the connector needs and
keep secret values in encrypted credential schemas.

Targets can have multiple credential profiles. Connector templates decide how
to expose profile selection in the UI, while token permissions always bind the
exact target/profile/action tuple that will run. Built-in SSH uses the same
target/profile/action model as structured connectors; runtime-backed features
receive a connector-profile capability-surface id from the owning adapter.

The 0.2 connector line is a clean baseline. Do not add compatibility branches
for pre-0.2 preview database layouts. Important old data belongs in a separate
versioned migration helper, not in runtime fallback code in the gateway.

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
- `GetActionList` action metadata for one target/profile, including stable
  action names plus non-empty labels, descriptions, and retry semantics
- `PrepareAction` validation and normalized action input
- `ExecuteAction` transport-specific execution

Connectors must not create their own permission, approval, history, or audit
pipeline. They return structured results; the shared gateway services persist
request state, output, errors, and audit records.
In the 0.2 baseline, `RuntimeContext.Events` is reserved/no-op and
`ActionResult.Metadata` is not persisted or returned through MCP. Put
operator- or AI-visible structured data in `ActionResult.Output`.

The shared action catalog completes an omitted retry policy conservatively:
`read` actions become `read_only`, while write, destructive, and
credential-sensitive actions become `non_idempotent`. Set an explicit
`RetryPolicy` only when the connector can make a stronger claim. Use
`idempotent` for an operation whose exact repetition converges on the same
remote state. Use `conditional` with `PreconditionFields` when the provider
atomically checks those fields during mutation. The schema validator rejects
unknown or duplicate precondition fields. Never describe a preflight read plus
an unguarded write as conditional; the check must travel with the mutation.
If the precondition is optional, keep the catalog policy `non_idempotent` and
set `PreparedAction.RetryPolicy` with `connectors.ConditionalRetryPolicy(...)`
only for prepared inputs that contain the actual provider-enforced guard.
The local `idempotency_key` prevents duplicate gateway request creation, not
duplicate remote execution after `outcome_unknown`.
For local UI actions, the browser retains an uncertain attempt's generated key
across reloads. A recognized request ID with `running` or `approval_pending`
does not release it. A validated definitive terminal response, including
replay, or a matching validated request observation can settle the identity.
A fresh definitive `4xx` rejection may release its new identity; carried retries
stay protected, and `outcome_unknown` requires explicit operator reconciliation.
Do not implement a connector-specific retry ledger.
The browser signs the canonical request fingerprint with HMAC-SHA-256 using a
non-extractable, origin-local signing key scoped to the database installation.
Its bounded IndexedDB ledger stores that signing key, the generated idempotency
key, keyed signature, and reservation/revision metadata; raw action input and
credential values do not enter browser storage. See the canonical
[browser retry ledger contract](../api/rest-api.md#history-and-connector-approvals).

For write dialogs, reuse `useConnectorMutationOwnership` from the shared
template directory and supply the connector's write-action names. Run writes
through its `run` callback and pass `exclusiveMutationActions` to the shared
action runner; this reserves ownership atomically before dispatch. Keep
ownership scoped to workspace and full `target_ref`, not a view, draft,
selected resource, or presentation session. Do not reset it when a dialog
closes or a read fails. The shared observer only releases confirmed terminal
requests; unknown outcomes retain their ledger identity for explicit operator
reconciliation. Add component tests for lost replies, remounts, draft changes,
workspace changes, and simultaneous admissions.

`ActionResult.Output` may use a typed Go struct, map, slice, pointer, or custom
JSON marshaler, but it must encode as JSON. Before persistence or external
projection, the gateway converts it to canonical JSON primitives, recursively
redacts every string leaf and declared sensitive field, and validates the final
redacted projection again. The global boundary rejects output above 4 MiB,
deeper than 32 levels, larger than 100,000 nodes, or containing a string or
object key above 1 MiB. Connector-specific `OutputHint` limits should normally
be much smaller and remain part of the connector's own execution contract.

The canonical redacted value is the single projection reused by encrypted
history and the MCP result. A connector must not depend on concrete Go types
surviving this boundary. If output cannot cross it after a remote action has
already executed, the request finishes as `outcome_unknown`; callers must
inspect state before retrying because the side effect may have completed.

Mutation transports must make the same distinction. A rejection received
before dispatch, or a definitive provider response after dispatch, may finish
as `failed`. Once any mutation bytes may have reached the remote service,
connection loss, timeout, malformed or incomplete success responses, and
failed post-write verification must finish as `outcome_unknown` with
`retry_safe: false`. Never automatically retry that result. Add transport-level
tests for both definite rejection and ambiguous post-dispatch failure.

The gateway owns request execution claims. It persists a runtime owner and
lease before execution, then atomically claims dispatch immediately before it
calls `ExecuteAction`. Connector implementations must not start remote work in
validation, preparation, schema, help, or action-list methods. If recovery has
already terminalized an expired request, the gateway does not call the
connector. Connectors should therefore classify only failures at and after
their own transport boundary; they must not implement a parallel request lease
or gateway recovery mechanism.

Target schemas must be non-secret. Use target schemas for endpoint metadata
such as host, port, database name, or API base URL. Use credential schemas for
passwords, tokens, private keys, tenant secrets, and anything that should be
vault-encrypted. If a credential schema uses a secret field type, mark that
field with `secret: true`; ambiguous secret fields fail registry validation.
Do not put defaults on secret credential fields.

If changing a target schema default would reinterpret an already stored target,
implement the optional `TargetConfigUpdateNormalizer` contract. It receives the
existing and submitted non-secret target configs before declarative defaults
are applied. Preserve only the connector-owned compatibility fields that need
it; new target creation must continue through the normal schema defaults.

The Connectors page manages a target and its default credential profile.
Additional profiles for the same target belong on the Credentials page. Token
permissions always bind one target, one credential profile, and one action.

Draft target tests before save are connector capabilities. SSH implements that
capability because host-key approval and remote key installation happen before
the local target/profile is persisted. Normal structured connectors should
implement saved profile tests through `TestableConnector`; do not add
connector-specific draft-test branches to generic route handlers without
designing a reusable contract first.

Action input schemas must not contain secret fields. Put passwords, API keys,
tokens, private keys, and tenant-specific secret material in credential profile
schemas so the gateway can encrypt and redact them consistently. `PrepareAction`
may validate references to credential profile metadata, but raw secrets are
available only through `RuntimeContext.Secrets` during approved execution.
Credential update handlers serialize read/decrypt/merge/write through the
shared Vault delivery coordinator, and secret writes use a monotonic database
revision compare-and-swap. Connector code must not read, merge, or persist a
credential envelope through a separate update path.
Prepared action payload keys must also avoid secret-looking field names such as
`password`, `token`, `api_key`, or `private_key`; the action service rejects
those payloads so connector secrets stay in credential profiles.
For connector-specific output fields that contain sensitive material, set
`ActionDefinition.OutputHint.SensitiveFields`. The gateway masks those field
names in structured output before returning MCP responses or persisting
history/audit payloads.

If an action intentionally generates a narrowly scoped, short-lived bearer
capability that must be returned intact, list its string field in
`ActionDefinition.OutputHint.TemporaryCapabilityFields`. Built-in token-pattern
redaction is skipped only for that declared field; operator custom redaction
still applies, and encrypted history retains the capability for the normal
retention period. Keep expiry bounded and test that no source credential is
present. Contributor-declared sensitive/capability overlap is rejected, while
gateway built-in sensitive names such as `token` and `secret` always remain
redacted and must not be declared as temporary capabilities.

For action inputs such as message keys, payloads, or headers whose arbitrary
content may itself be sensitive, list the corresponding schema field names in
`ActionDefinition.SensitiveInputFields`. The gateway persists and displays
redacted input JSON, while the raw prepared execution payload is encrypted
separately for the action runner. Tests for a connector should prove that
realistic secret-looking input/output values are masked in MCP responses and
history.

Approval-required actions store a context snapshot when the request is created.
That snapshot includes token validity, permission rule, target/profile public
metadata, profile revision, encrypted secret revision, connector kind/version,
action definition hash, and action payload hash. If those values drift before
the operator clicks Run, the request becomes `stale` and the AI must submit a
fresh action request.

Synchronous connector actions can return `completed`, `failed`, or `error`
directly from `ExecuteAction`. Long-running `running` actions require an
explicit connector-owned runtime adapter built on the typed
`internal/gatewayconnectorapi` contracts. The generic gateway resolves that adapter so
polling, output finalization, redaction, history sync, and MCP assistant hints
stay centralized. Do not invent connector-local polling tables.

`RuntimeContext.Capabilities` is reserved for reviewed gateway-owned runtime
adapters. Normal structured connectors should not depend on arbitrary gateway
services. If a new connector needs a capability similar to live terminals,
file transfer, or async progress, first define a reusable typed adapter
contract and document why the shared action runner is not enough.

Session environment delivery is an opt-in live-runtime capability. A connector
that advertises `SessionEnvironmentCapabilityName` must implement the complete
`SessionEnvironmentCapability` contract, including a stable capability version
and `SessionEnvironmentPeerIdentityRequired`. When peer identity is required,
the live-console adapter must return at least one normalized expected identity
and the opened runtime must report the matching actual identity. Missing
identity support fails closed. A connector may return `false` only when peer
identity is genuinely not applicable to its transport and that boundary is
documented and reviewed.

Use existing reviewed capabilities before inventing connector-specific routes:

- `NetworkTransport` opens direct or Over SSH TCP connections for protocol
  connectors such as Postgres, Redis, and RabbitMQ.
- `CommandTransport` runs bounded connector-owned command templates through a
  reviewed connector transport such as SSH. Docker uses this for fixed Docker
  CLI templates while keeping arbitrary host-level shell and raw Docker command
  execution out of scope. Scoped container exec is modeled as a connector action
  and must enforce the Docker credential profile scope first.

Do not import the SSH connector package from another connector. Ask for a
generic capability such as `NetworkTransport` or `CommandTransport`; the
gateway resolves the selected transport profile.

Connector-owned HTTP adapter routes are exceptional and declarative. A
`gatewayconnectorapi.RouteDefinition` must declare `RoutePolicyUIRead` for a
safe `GET` or `HEAD` route, or `RoutePolicyUIMutation` for a state-changing
route. The connector registry and API transport both reject an empty, unknown,
or method-incompatible policy. Routes must live below
`/api/connectors/<connector-kind>/`; registration rejects core, MCP, lifecycle,
streaming, and another connector's namespace. This policy selects the shared
local UI/session and CSRF boundary; it does not replace action permission,
approval, audit, or connector-side authorization. Prefer an existing generic
action or capability over adding a custom route.

Every Over SSH prepared action must declare that selected transport through
`NetworkTransportDependencies` or `CommandTransportDependencies`. The gateway
then binds approval to that exact target/profile snapshot, rejects attempts to
use an undeclared transport, and revalidates configuration plus credential
revision under the Vault delivery gate before a socket or command is opened.
Connector code must not construct a different transport ref during execution.

For a protocol that supports both plaintext and verified TLS, make new remote
Direct targets fail-safe by default. Reuse `UseVerifiedTLSByDefault` for an
`auto` mode instead of copying hostname heuristics into a connector. The shared
policy tells the connector when verified TLS should be the default for a remote
Direct host. For loopback, supported local-container aliases, and Over SSH,
the connector must choose its own protocol-safe fallback; this helper does not
authorize plaintext transport. Connector code still owns protocol-specific TLS
setup, ports, and error classification. Keep any explicit plaintext option
limited to intentionally local deployments, preserve existing saved choices,
and test new, legacy, explicit, local, remote, and Over SSH configurations
separately.

## Frontend Templates

Add templates under:

```txt
frontend/src/connectors/templates/<kind>/
```

The folder is discovered automatically. Do not manually edit
`frontend/src/connectors/templates/registry.tsx` or
`frontend/src/connectors/templates/catalog.ts` for a normal connector. The Vite
bundle discovers `index.ts` and `metadata.json` through `import.meta.glob`.

Expected files:

- `metadata.json`: label, summary, icon, version, badge tone, profile lifecycle,
  and optional transport-provider capability metadata
- `model.ts`: display helpers, target subtitle, profile labels, operations, and
  whether the target uses a live terminal
- `form.tsx`: add/edit connector target form
- `credential-form.tsx`: credential profile form
- `connector-family.tsx`: native target editor and operation registration
- `credential-family.tsx`: native credential editor and row registration
- `list-item.tsx`: connector-specific row operations
- `console.tsx`: connector console/activity surface and toolbar actions

Template slots:

| File or export         | Required | Use it for                                                                                                                |
| ---------------------- | -------: | ------------------------------------------------------------------------------------------------------------------------- |
| `metadata.json`        |      yes | Connector label, version, summary, icon, and badge tone.                                                                  |
| `model.ts`             |      yes | Display helpers, target/profile labels, endpoint text, test/delete behavior, and whether the target uses a live terminal. |
| `form.tsx`             |      yes | Add/edit target fields for the connector target schema.                                                                   |
| `credential-form.tsx`  |      yes | Add/edit credential profile fields for the credential schema.                                                             |
| `list-item.tsx`        |      yes | Connector-specific row operations on the Connectors page. Do not put generic Edit/Delete/Test actions here.               |
| `console.tsx`          |      yes | Structured activity surface or live-console template for the Console page.                                                |
| `CredentialRowActions` | optional | Extra credential-row actions, such as copying an SSH install command.                                                     |
| `ToolbarActions`       | optional | Connector-specific Console toolbar actions, such as Files or Bulk for SSH.                                                |
| `Operations`           | optional | Connector-specific dialogs/operations launched from list rows.                                                            |

Each `index.ts` must preserve its native model/form inference while checking
the shared console slots with `satisfies ConsoleTemplateContract` from
`frontend/src/connectors/templates/console-template-contract.d.ts`. Wrap its
default export in `defineConsoleTemplate` from `_shared/console-template`.
The factory freezes the native registration and captures only `Console` and
optional `ToolbarActions`; copied or structural lookalikes are rejected. The
common registry does not expose native forms or models. Editor and credential
pages use their captured family registries; console presentation reads and
session recovery use their respective captured registries. `Console`
accepts `ConsoleWorkspaceSlotProps`; optional `ToolbarActions` accepts
`ConsoleToolbarSlotProps`. Do not cast a native component to this contract.
Decode connector-owned fields from the public target's `config` and `public`
records inside the connector. Structured consoles use
`structuredConsoleSlotSession`; runtime-backed consoles use
`liveConsoleSlotSession` and preserve the independent target, profile, runtime,
and named-session identities. A shared workspace session must never be assumed
to be the connector's native session shape merely because the registry loads it.
The workspace contains console/toolbar render failures in independent generic
slot boundaries. A failed slot does not replace surrounding navigation or the
other slot. The boundary resets for a new target/profile or updated saved target,
and permits an explicit retry without displaying arbitrary exception payloads.

Credential pages use shared presentation components under
`frontend/src/connectors/editor/`: `credential-menu.tsx`, `credential-row.tsx`,
and `credential-delete-dialog.tsx`. The generic row only consumes display
fields. A captured credential family retains the original native row for
Edit/Delete callbacks instead of exposing its payload in presentation props.
Keep credential-specific fields, secret handling, and extra operations in the
connector; render its optional `CredentialRowActions` slot into the generic
row's `operations` prop. Do not make the shared row resolve or inspect native
credential fields. The shared delete dialog presents connector-provided
metadata and delegates confirmation; any connector-provisioned external
cleanup remains in the connector model.

Define native credential workspaces with `defineCredentialFamily` from
`templates/_shared/credential-family-registration.ts`. Keep the definition in
the connector directory and export its registration as `credentialFamily`
from `index.ts`. The definition captures the native state, full row, target,
and operation types before the registry sees the common UI interface. The
factory returns a frozen registration; dynamic module consumers must check
`isCredentialFamilyRegistration`, not assert an unknown glob result has model
or component call signatures.

Supply `decodeTargets`, `emptyState`, `model`, `rows`, `displayRow`, and
`renderForm`. Optional `renderOperations` and `deleteDialog` callbacks receive
the original native row. Decode consumed connector-owned `config` and profile
`public` fields from validated inventory envelopes; preserve target, project,
profile, and runtime identifiers independently. `credentialDisplayRow` projects
only presentation fields and does not copy native payloads. Construct form
props directly from the native model and typed editor, so incompatible state,
callbacks, or form components fail compilation.

`templates/credential-registry.ts` discovers the named `credentialFamily`
exports, verifies their factory registrations and catalog/path identity, and
binds the definitions to `captureCredentialFamily`; connector
templates do not import editor orchestration at runtime. The common host gets
only rendered rows, lifecycle status, and stable `openCreate`/`close` commands.
The typed Credentials page uses this registry rather than calling heterogeneous
native model functions. The host coordinates active-family status, row counts,
and exclusive draft selection. Native-family render failures are contained in
table-compatible boundaries, reset by refreshed inventory/resource inputs,
without displaying arbitrary exception details or breaking other families.
The captured component owns its editor and delete dialogs, whose DOM is
portaled outside the table. Commands, native form writes, and confirmations
must respect cross-family busy state, unmount, and retired draft guards. See
the native `ssh/credential-family.tsx`, `redis/credential-family.tsx`, and
`postgres/credential-family.tsx`
implementations and their real-controller component tests.

Define target editors with `defineConnectorFamily` from
`templates/_shared/connector-family-registration.ts`, and export the frozen
registration as `connectorFamily` from the connector's `index.ts`. Supply the
native form, profile, target, credential resource, active credential, and
operation types. These types remain correlated inside the connector-owned
definition; a shared host must not replace them with a broad model interface
or cast an unknown glob result to a native model.

The definition provides inventory/credential decoding, `emptyForm`,
`emptyOperation`, the native editor and table models, delete-dialog metadata,
and `renderForm`, `renderRowActions`, and `renderOperations`. Reuse the same
owned inventory decoder as the credential family where their target schemas
match. Render the form with the supplied typed `onChange` callback. Row and
operation render callbacks receive the matching native target/profile and
operation setters, never another connector's payload.

`templates/connector-family-registry.ts` validates the named registration and
its catalog/path identity before applying `captureConnectorFamily`. The
generic Connectors page composes these providers and dispatches only
`openCreate`, `openEdit`, `test`, `requestDelete`, and `close`. The table sees
display/model projections and a captured row-actions slot; it does not receive
or dispatch native operation payloads. Existing-target commands remain
registered during backend catalog/detail failures, while creation choices are
limited to available catalog entries.

The capture owns native editor state, selected-profile resolution, recovery,
dialogs, and operation generations. Confirmations and delayed completions must
not mutate a closed, replaced, or unmounted operation. Keep these guards when
adding a new operation, and cover create, second-profile edit/test, local
deletion, project preservation, and any connector-specific cancellation or
recovery with real-controller tests. The connector-owned registration must not
import its captured registry, the page, or editor orchestration at runtime;
type-only imports of editor contracts are allowed.

Define console presentation with `captureConsolePresentation` from
`templates/_shared/console-presentation.ts`. Export the captured result as
`consoleModel` from the connector's `index.ts`, with the same `kind` as its
directory and metadata. Keep the native target decoder and model inside the
connector; the capture preserves their correlated types without publishing the
form model or mutation methods to common UI code.

The projection provides target display name, subtitle, profile label,
live-console capability, and recoverable running actions. Decode only the native
config/public fields these methods need. Do not manufacture target, profile, or
runtime IDs just to satisfy a form/editor type: presentation types should require
only presentation data. If a subtitle uses live-runtime fallback fields, validate
those fields in the native `subtitle` callback. Reuse an existing console decoder
where the schema matches. Database connectors share
`captureDatabaseConsolePresentation` for their common endpoint fields.

`templates/console-model-registry.ts` verifies factory provenance and exact
catalog/path identity before exposing these projections to navigation, History,
and permission UI. Plain objects, copied registrations, missing kinds, and
path-mismatched models are rejected. Keep registrations frozen and add tests for
defaults, live-runtime fallbacks, malformed data, and identity preservation.
Permission rows without a full console reference may query the connector's
default capability without creating a synthetic target reference.

For runtime-backed consoles, provide a native `runtimeTarget` callback using
`captureConsoleRuntimeProjection`. Its decoder receives a gateway target with a
verified positive runtime identity. Narrow native runtime projection inputs to
the fields they use, rather than requiring the editor's CRUD target shape.
The shared capture rejects a native projection that substitutes a target or
profile ID for `runtime_id`, and retains the original gateway target in the
runtime result. Native config/public validation still belongs to the connector.
The captured model may also expose the native `loadCredentialResources` read
service; preserve all caller options, including cancellation and poll timeout.
Neither service exposes credential or target mutation methods to shared code.

Every native `index.ts` also exports `consoleRecovery`: explicitly `null` when
unsupported, or a `captureConsoleSessionRecovery` registration when session
startup errors require a native dialog. An absent named export is not supported
by the eager browser registry. The native callback selects a recoverable error
and an exact retry runtime, then supplies its typed operation and dialog render
callback. The capture stores that operation privately; the common console host
receives only an opaque, connector-scoped envelope. Closing, replacing, or
unmounting the dialog retires its callbacks. Test fingerprint approval/retry,
cancelled and stale completions, factory provenance, and unrelated errors with
the real console consumer as well as the native template.

Allowed metadata icons are `database`, `key`, `mail`, and `server`. Add another icon
only when the shared template registry and docs are updated together.

A connector that implements the backend `TCPTransportAdapter` may advertise
the matching frontend transport profile selector with a `network_transport`
descriptor. Its `mode` must be the backend-owned `connection_mode` value and
its labels are display text. Every endpoint field must read from public
`target.name`, `target.config.*`, `profile.label`, or `profile.public.*` data
and declare a non-secret scalar `fallback` used when that value is absent.
Advertising this metadata without the matching backend adapter is a contract
error and must be covered by connector conformance tests. Two transport
providers may share a mode only when their complete descriptors are identical;
conflicting labels or endpoint templates fail the frontend registry contract.

```json
{
  "network_transport": {
    "mode": "over_ssh",
    "label": "SSH",
    "option_label": "Over an SSH connector profile",
    "profile_label": "SSH connector profile",
    "profile_endpoint": {
      "fields": [
        { "path": "target.config.host", "fallback": "host" },
        { "path": "target.config.port", "fallback": 22 }
      ],
      "separator": ":"
    }
  }
}
```

`profile_endpoint` keeps provider-specific endpoint identity out of the shared
transport selector. Each field reads a public target/profile path and may
provide a non-secret display fallback; the shared UI joins those values with
the declared separator.

`model.ts` is the connector UI contract. Keep these exports small and
connector-local:

Set `profile_lifecycle` in `metadata.json` to `standard` when the model uses
the shared target/profile CRUD lifecycle. Use `custom` only for connectors
that own materially different provisioning or cleanup behavior. The
architecture gate discovers every template directory and fails when this
declaration is missing or a standard model bypasses the shared lifecycle.

For the standard target plus credential-profile lifecycle, use
`createTargetProfileLifecycle` and `connectorCredentialRows` from
`frontend/src/connectors/templates/_shared/target-profile-lifecycle.ts`. Supply
connector-owned target and profile payload builders; do not copy the generic
create/update/delete/test routes into each model. Keep a custom lifecycle only
when the remote system has materially different cleanup or provisioning
semantics, and cover that exception with focused tests.

| Export                                                                                                                          |          Required | Purpose                                                                                             |
| ------------------------------------------------------------------------------------------------------------------------------- | ----------------: | --------------------------------------------------------------------------------------------------- |
| `emptyForm`                                                                                                                     |               yes | Initial add-target form state.                                                                      |
| `formFromTarget`                                                                                                                |               yes | Convert saved target/profile data into edit form state.                                             |
| `save`                                                                                                                          |               yes | Create or update the target plus default profile. Use shared target/profile helpers where possible. |
| `deleteTarget`                                                                                                                  |               yes | Invoke generic target delete/archive, plus connector-specific cleanup options when needed.          |
| `test`                                                                                                                          |               yes | Run the saved target/profile connection test.                                                       |
| `targetDisplayName` / `targetSubtitle` / `targetEndpoint`                                                                       |               yes | Labels used by generic target lists and console headers.                                            |
| `targetProfileLabel`                                                                                                            |               yes | Profile label shown in the Console token panel.                                                     |
| `activeCredential`                                                                                                              |               yes | Pick the credential/profile shown as active for a selected target profile.                          |
| `submitDisabled` / `submitLabel`                                                                                                |               yes | Add/edit form affordances for connector-specific validation and copy.                               |
| `syncForm`                                                                                                                      |               yes | Reconcile connector form state when async resources, such as credential rows, load.                 |
| `usesLiveConsole`                                                                                                               |               yes | `true` only for adapters that own a live terminal runtime.                                          |
| `deleteDialog`                                                                                                                  |               yes | Copy and action buttons for target deletion.                                                        |
| `emptyCredentialState`, `credentialStateFromRow`, `credentialFormProps`, `saveCredential`, `deleteCredential`, `credentialRows` |               yes | Generic Credentials page integration.                                                               |
| `credentialHint`, `canEdit`, `canDelete`                                                                                        |               yes | Generic row affordances.                                                                            |
| `operationFromError`                                                                                                            |          optional | Convert connector-specific API errors into connector-owned retry operations.                        |
| `hostKeyActionFromError`, `resumeHostKeyAction`                                                                                 | optional SSH-only | SSH uses these for host-key approval retry. Non-SSH connectors should not add no-op host-key stubs. |

Connection tests should return stable generic statuses: `ok`, `failed_config`,
`failed_network`, `failed_tls`, `failed_auth`, `failed_permission`, or
`unknown_error`. Invalid target/profile metadata is `failed_config`; do not
mislabel deterministic validation failures as unknown network errors.

Connector templates may add optional exports for connector-owned operations,
but generic Test/Edit/Delete and permission/history behavior must stay in the
shared pages and stores.

The frontend registry validates required template slots and required `model.ts`
exports at runtime during tests. If a connector folder omits a required slot,
uses an unsupported metadata icon, or has metadata whose `kind` does not match
the folder name, `npm test` fails before the UI can silently render a partial
connector.

Register the connector backend and add frontend template files:

```txt
backend/internal/connectors/builtin/catalogdata/register.go
backend/internal/connectors/builtin/catalogruntime/register.go
backend/internal/connectors/builtin/adaptercontainers/register.go
backend/internal/connectors/builtin/adapterresources/register.go
frontend/src/connectors/templates/<kind>/metadata.json
frontend/src/connectors/templates/<kind>/index.ts
```

Backend registration is explicit in the Go binary and split by ownership:

- `catalogdata/register.go` registers structured protocol connectors that use
  the shared synchronous action runner.
- `catalogruntime/register.go` registers connectors that expose an approved
  runtime-backed capability surface.
- `adaptercontainers/register.go` registers runtime adapters for container
  resource surfaces.
- `adapterresources/register.go` registers runtime adapters for resource and
  file-transfer surfaces.
- `registry.go` only aggregates those registration groups into immutable
  connector and adapter catalogs. It must not gain connector-specific
  construction branches.

Only add an adapter registration when the connector implements a reviewed
optional contract from `internal/gatewayconnectorapi`. The gateway receives
both catalogs through `builtin.NewCatalog`; package `init()` registration and
blank side-effect imports are not allowed. Frontend registration is
folder-based and auto-discovered. Architecture tests require the backend
catalog, frontend template kinds, and generated connector catalog to remain
identical. Adding a connector should require connector files, the appropriate
owned registration entry, tests, and docs, but it should not require new
generic route handlers, permission tables, history tables, audit tables, or
MCP tool families.

Connector versions are part of approval-context drift checks. Bump the backend
connector `Version()` and the frontend `metadata.json` version whenever you add
or rename actions, change action input/output schemas, change target/profile
schema semantics, or materially change execution behavior. Pure copy or visual
polish can stay on the same connector version.

Route-level pages should render through the template registry. Avoid adding
new `if kind === "redis"` branches to generic pages.

## Normal Connector PR Boundary

Most connector PRs should be normal structured connectors. They are welcome
when they touch only connector-owned code plus the explicit registration,
tests, and docs needed to ship the built-in:

Expected for a normal connector:

- `backend/internal/connectors/<kind>/`
- backend registration in the appropriate
  `backend/internal/connectors/builtin/catalog*/register.go` owner
- backend registry/contract tests for the shipped connector set
- `frontend/src/connectors/templates/<kind>/`
- frontend template registry/smoke tests
- README, REST/MCP, security, or connector-specific docs when behavior changes

Not expected for a normal connector:

- new token permission tables
- new approval request tables
- connector-specific history or audit tables
- a new MCP tool family
- project-specific tables, routes, filters, or token-scope checks
- route-level branches such as `if kind == "redis"` or `if kind === "redis"`
- connector-specific command/session/file-transfer tables
- direct imports of `internal/api`

If a connector cannot fit the normal structured path, stop and write a design
note first. Runtime-integrated connectors are maintainer-reviewed exceptions
for reusable gateway capabilities such as live terminals, SFTP transfer,
host-key approval, or another long-running local runtime surface. Those
capabilities must be expressed once through typed `internal/gatewayconnectorapi`
contracts and then wired through generic handlers; they must not become
connector-local shortcuts.

Schema defaults are declarative UI hints and validation aids. Connector code
must still normalize defaults in `PrepareAction` or `ExecuteAction` before
building payloads, opening sockets, or running transport-specific logic.

Use `Field.PreserveWhitespace` (`preserve_whitespace: true`) only for opaque
`string` or `multiline` identities and payloads such as S3 object keys or Kafka
message bytes. It distinguishes a whitespace-only string from an absent value;
the empty string still follows required/default rules. Other fields retain
their existing empty-value semantics. Credential validation honors the same
flag for supported string and multiline fields. Changing it changes the action
catalog and approval-context hash. Test the generic action/API path, not only
connector preparation, when adding it.

Remote transfer identities need not be filesystem paths. An adapter can implement
`connectorapi.FileTransferPathPolicy` to own locator validation, upload joining,
parent navigation, and ZIP preflight. Shared handlers and transfer storage must
not normalize those locators again. Local staging paths and safe download names
remain separate. Frontend transfer options accept matching path callbacks; do
not add connector-kind branches to shared transfer components.

Operator-only connector operations should use reviewed optional contracts
instead of adding connector-specific routes. For example, a connector that can
create external credentials implements `CredentialProvisioner`, and a connector
that can produce/restore backup artifacts implements `BackupRestorer`. Core owns
HTTP upload/download, confirmation, vault persistence, and audit; the connector
owns only the external service-specific work.

A structured connector needing a durable domain journal can implement
`gatewayconnectorapi.ScopedResourceCapabilityProvider` in its own adapter. Its
`ScopedResourceRuntime` exposes only `CredentialResources(resourceKind)` for the
core-bound connector kind. Return a typed `connectors.RuntimeCapability` for the
domain service, as the Postgres `apiadapter` does for its role journal. This
resource-only contract does not require a console runtime, execution-surface ID,
principal resolver, action mutation port, database handle or Vault handle. Core
still owns workspace exclusion, resource encryption, scope enforcement and
retirement; the connector owns its journal schema, transitions and remote proof.
Do not use `AsyncRuntimeAdapter` just to obtain connector-scoped persistence.

A managed credential can optionally implement
`ProvisionedCredentialCleanupEvidence` to confirm an already-completed cleanup
before core looks up the original administrator. Its adapter must implement
`EvidenceCapabilityProvider`, which receives only `EvidenceResourceRuntime`.
The concrete resource readers expose public `Get` only, not mutable stores,
`GetSecret`, console/session ports or transports. Return a separate read-only
domain capability, not the mutable lifecycle journal. Core still holds exclusive
lifecycle admission across fresh target/profile reads, evidence validation,
local retirement and audit. A nil evidence result requires the usual authenticated
remote cleanup; errors or nonterminal results stop deletion. Test missing/changed
identity, uncertain records, cancellation during projection and retained readers
after workspace retirement. Never infer completed cleanup from remote absence.

When an authenticated local operator operation needs one saved credential,
implement the optional `CredentialTargetOperationRunner` in the connector's
adapter. `SupportsCredentialTargetOperation` must be stable and side-effect-free.
This contract is independent of `TargetOperationRunner`: a connector does not
need an ordinary data runtime or console adapter to implement it. Requests to
the existing target operation route contain a canonical decimal string
`profile_id` and an `input` object. Core holds exclusive lifecycle admission,
loads the current target/profile, validates the credential kind and constructs
the credential boundary. It composes transport and scoped-resource capabilities,
not the action adapter's runtime capabilities. There is no database, Vault or
profile-publication handle in this operation contract.

The adapter owns input validation, external identity proof and its domain
transitions. Core projects required and best-effort audit payloads, errors and
responses through the same mutable boundary, including registered derived
sensitive values. Keep required pre-dispatch audits attached to a bounded
operation context; failed audit must prevent external admission. Detached
terminal audit must have its own short deadline, and lost terminal evidence
must never be reported as confirmed success. Test selected nondefault profiles,
workspace retirement, late cancellation, corrupt/stale snapshots, disabled
optional redaction and retained exclusion through audit/response completion.

## Built-In Example: Redis / Valkey

The built-in Redis / Valkey connector adds only protocol/product-specific behavior:

- target fields such as server product, connection mode, host, port, database
  index, TLS mode, and optional SSH transport target ref
- credential profile fields such as username/password or token
- actions such as `ping`, `info`, `scan_keys`, `get_key`, `set_string`,
  `expire_key`, and `delete_keys`
- connector help that explains safe key inspection and the standalone RESP2
  boundary
- UI templates for Redis / Valkey target rows, credential profiles, and key-browser
  console output

It should not add a Redis/Valkey-specific token permission table, approval table,
history page, audit route, MCP tool family, or global UI page.

Redis / Valkey checklist:

- `backend/internal/connectors/redis/redis.go`
- `backend/internal/connectors/redis/client.go`
- `backend/internal/connectors/redis/redis_test.go`
- connector catalog registration in either
  `backend/internal/connectors/builtin/catalogdata/register.go` or
  `catalogruntime/register.go`, plus its focused registration test
- adapter registration in `adaptercontainers/register.go` or
  `adapterresources/register.go` only when the connector implements one of the
  optional runtime adapter contracts, plus its focused registration test
- backend route tests if the built-in connector list or inventory expectations
  are exact
- `frontend/src/connectors/templates/redis/metadata.json`
- `frontend/src/connectors/templates/redis/index.ts`
- `frontend/src/connectors/templates/redis/model.ts`
- `frontend/src/connectors/templates/redis/form.tsx`
- `frontend/src/connectors/templates/redis/credential-form.tsx`
- `frontend/src/connectors/templates/redis/list-item.tsx`
- `frontend/src/connectors/templates/redis/console.tsx`
- frontend smoke/runtime tests that assert the shipped connector folders
- README, REST/MCP docs, and connector-specific safety notes

`backend/internal/connectors/builtin/registry.go` is only the composition
aggregator for those owned registration groups. Adding a normal connector must
not add connector-specific construction logic there.

Valkey compatibility remains inside this connector because both products use
the same bounded action catalog and RESP2 transport. The user-selected
`server_family` affects product labels and approval context, while the technical
connector kind and target refs remain `redis`. Do not add a duplicate `valkey`
connector folder, catalog registration, generic route branch, permission
table, or MCP wrapper merely to change the product identity.

Redis key identities cross the JSON action boundary as exact UTF-8 strings.
Whitespace, NUL bytes, valid Unicode, and literal U+FFFD are not normalized.
Non-UTF-8 binary keys are deliberately unsupported: preparation and execution
reject them before any key command, and `scan_keys` fails the whole response
without partial keys or a misleading continuation if a returned key is invalid.
Use a narrower `MATCH` pattern to exclude unsupported keys. Never substitute
replacement characters, truncate identities, or silently skip a scan entry;
those changes could make a later read, TTL update, write, or delete target a
different key. The same connector-owned validator guards scan output and all
key action inputs. Values are bounded previews, not key identities.

## Built-In Example: RabbitMQ

The built-in RabbitMQ connector follows the same normal structured connector
path as Redis:

- target fields such as connection mode, automatic/explicit HTTP scheme, host,
  port, default vhost, and
  optional SSH transport target ref
- credential profile fields for RabbitMQ Management API username/password
- actions such as `overview`, `list_vhosts`, `list_queues`, `get_queue`,
  `list_bindings`, `peek_messages`, and `publish_message`
- connector help that explains payload sensitivity, bounded peeking, and
  write-scoped publishing
- UI templates for RabbitMQ target rows, credential profiles, and queue-browser
  console output

It should not add RabbitMQ-specific permission tables, approval tables, history
pages, audit routes, MCP tool families, or global UI pages.

Vhost, queue, exchange and routing-key fields use `PreserveWhitespace` and a
connector-owned identity validator at preparation and execution. Discovery and
selection retain exact strings; picker labels may quote names but must not
become the submitted value. Optional empty defaults are distinct from
whitespace-only identities. The bounded Management API response decoder uses
the shared lossless JSON validator before typed decoding, and malformed
responses after a mutation preserve unknown-outcome classification. Owner
tests cover schema-to-prepared-JSON-to-HTTP dispatch; the required isolated
service fixture proves padded names do not route to their unpadded siblings.

## Built-In Example: Mail

Mail is a normal structured connector. Its target owns IMAP/SMTP endpoints,
TLS modes, Direct or Over SSH transport, and optional recipient-domain policy.
Its credential profile owns mailbox identity, encrypted app passwords, and
read/mutation folder policy. Protocol code and the mailbox workspace stay in
the Mail connector directories.

Mail demonstrates several connector rules:

- reads use bounded IMAP UID references and peek semantics;
- `mark_read` and `mark_unread` are explicit write actions;
- move/archive/delete revalidate UIDVALIDITY and folder policy at execution;
- outbound bodies and recipients are sensitive action input, while the generic
  approval preview deliberately shows the complete bounded message;
- SMTP unknown-delivery state is connector output, not a connector-specific
  retry pipeline;
- hostile incoming content is normalized to safe text and remains labeled as
  untrusted data;
- no Mail-specific permission, approval, history, audit, REST, or MCP tool
  family is introduced.

Use [Mail Connector](../setup/mail.md) for the operator-facing protocol and
security boundary.

## Postgres Safety Boundary

The built-in Postgres connector is intentionally conservative. `query_readonly`
rejects obvious write statements, enforces a SQL size limit, executes with a
read-only transaction, permits only an audited set of read-only `pg_catalog`
functions, restricts read-query name resolution to `pg_catalog`, applies a
statement timeout, caps row count, and caps returned output bytes before
MCP/history persistence. Application relations must be schema-qualified, for
example `public.orders`. Unknown and extension-provided function calls fail
closed. Postgres credential
provisioning is a UI operator flow, not an MCP action: it uses an admin profile
to create a scoped database role with a random password, then stores the
resulting credential profile encrypted in AIPermission. Those controls are not a
substitute for database-level least privilege. Use dedicated read-only roles for
AI profiles and prefer `approval_required` for exploratory SQL. Postgres
backup/restore is also a local UI operator flow: backup uses `pg_dump`, restore
uses `psql` with `ON_ERROR_STOP` and requests single-transaction mode, and
restore requires typed target-name confirmation. Explicit transaction control
inside arbitrary SQL can escape that wrapper, so connector code must not claim
the restore is fully atomic. The restore disables `psqlrc` files, accepts only
one canonical matching `\\restrict TOKEN` / `\\unrestrict TOKEN` pair, and
requires a one-time end-of-stream marker before reporting success. A restore
artifact with top-level transaction control remains `outcome_unknown` even when
the marker is observed. Core hashes the full
artifact and persists an idempotency claim before calling the connector. The
Postgres adapter rejects psql meta-commands before dispatch; SQL transaction
control can still make every post-start failure or missing marker terminal
`outcome_unknown`. Callers inspect database state instead of retrying blindly.
The generic profile restore handler owns the durable operation claim and
required terminal audit event for success, failure, cancellation, and uncertain
outcomes. If the transaction response is lost, replay uses the durable operation
identity instead of dispatching again. An audit write failure leaves a durable
pending-audit marker that workspace recovery finishes atomically. Core holds an
exclusive target/profile mutation gate for the full restore and retains a
non-secret replay tombstone for 30 days after history retention removes the
detailed terminal operation. Connector implementations must not add a parallel
locking or idempotency path.

File-transfer connectors that create remote staging artifacts implement
`RemoteStagingRecoveryAdapter`. The connector first creates an exclusively owned
empty staging namespace, core then records the opaque file reference, and only
after that durable record exists may plaintext be created inside the namespace.
Core owns restart discovery, per-candidate deadlines, and bounded periodic
retries, and asks only the owning connector to validate and remove its artifact.
Connector-specific paths or cleanup commands must not leak into the generic
transfer runtime. Recovery runs asynchronously under the workspace lifecycle,
while retention and target or profile mutation preserve the cleanup identity
until reconciliation finishes.
Local staging is namespaced by the durable local database-copy identity, which
rotates when a backup is imported. Connector code must request paths through
the generic transfer runtime and must not share or scavenge process-wide
staging directories, even when two local database copies retain the same
backup workspace UUID.

## ClickHouse Safety Boundary

The built-in ClickHouse connector is a normal structured connector. Its native
protocol driver, target/profile schemas, metadata SQL, and action execution live
under `backend/internal/connectors/clickhouse`; its UI lives under
`frontend/src/connectors/templates/clickhouse`. It reuses the generic network
transport, shared read-only SQL validation, bounded SQL result builder,
database connector model factory, and structured SQL console without adding
ClickHouse branches to generic routes, pages, permissions, history, audit, or
MCP tools.

`query_readonly` accepts one `SELECT`, `WITH`, `SHOW`, or `EXPLAIN` statement,
sets ClickHouse `readonly=1`, and caps timeout, rows, cell values, and total
output. These controls do not replace ClickHouse grants. Contributors and
operators must still use a dedicated read-only database user and should default
exploratory query access to `approval_required`.

## Tests

Add focused tests for:

- target schema validation
- credential profile validation
- action list visibility for a real target/profile
- target schema rejection for secret fields
- action input schema rejection for secret fields
- secret credential schema rejection when defaults are present
- connector-specific `OutputHint.SensitiveFields` redaction
- bounded `TemporaryCapabilityFields` preservation without source credential
  leakage
- connector-specific `SensitiveInputFields` redaction while the encrypted
  execution envelope preserves the exact input
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
- connector registration in the appropriate
  `backend/internal/connectors/builtin/catalogdata/register.go` or
  `catalogruntime/register.go` owner
- runtime-backed adapter constructor and registration in the appropriate
  `adaptercontainers/register.go` or `adapterresources/register.go` owner only
  when a reviewed `connectorapi` adapter is required
- focused registration tests beside every changed registration owner, plus
  aggregate catalog coverage in `backend/internal/connectors/builtin/`
- frontend templates under `frontend/src/connectors/templates/<kind>/`
- frontend `metadata.json` and `index.ts` auto-discovery through the template
  registry and catalog loaders
- frontend smoke coverage in `frontend/src/lib/app.smoke.test.ts`
- frontend runtime registry coverage that imports/evaluates the template
  registry module
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
