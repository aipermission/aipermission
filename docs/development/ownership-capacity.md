# Ownership And Capacity

Use this map before adding behavior to an owner near its source or test limit.
The [contributor map](contributor-map.md) identifies entry points; this page
identifies which responsibilities must remain together and which must not.
The executable authority is [maintenance-policy.json](../../maintenance-policy.json)
and `node scripts/maintenance-budget-check.js`, not the snapshot numbers below.

## Five High-Pressure Production Areas

The before column is the released v0.2.64 source, not an estimated review count.
The after column is a local hardening snapshot. Counts include physical source
lines, exclude Go tests, and do not combine separate Go packages. Reducing a
parent's count does not mean the complete tree or behavior became smaller.

| Area | Before | After | Unchanged parent/file limit | Cohesive responsibility moved |
| --- | ---: | ---: | ---: | --- |
| `internal/backups` | 3499 | 3253 | 3500 | Reflected credential rejection in `serviceboundary`; durable upload identity/transitions in `uploadoperation` |
| `internal/connectormanagement` | 3488 | 3314 | 3500 | Credential schema, secret merge/validation and encryption invocation in `profileinput` |
| `internal/db` | 2690 | 2080 | 2700 | Ordered immutable released definitions in `baselineschema`, not migration policy |
| `internal/console` | 3287 | 2949 | 3300 | Pure input observation/classification in `manualinput`, not session authorization or persistence |
| `connectors/ssh/execution/file_transfer.go` | 844 | 715 | 850 | Bounded complete stat metadata in connector-local `remotemetadata`, not transfer commit policy |

These are behavior boundaries, not new generic dispatch paths. Direct callers
use the child model and operation; root compatibility aliases and preparation
forwarders were not added. New owners have stricter local source limits and
explicit coverage floors. Existing parent limits and floors are not relaxed.
Near-limit warnings still apply, including to tests and to some new owners.

## Data, Policy, I/O And Projection

Paths below are relative to `backend/internal`, except explicitly marked
frontend or tooling paths. A row can have several I/O adapters, but only one
owner for each decision. An HTTP projection must not become another policy
implementation, and a pure helper must not gain access to a workspace secret.

| Area | Data owner | Policy owner | I/O owner | Projection owner |
| --- | --- | --- | --- | --- |
| Backups | `backups/store.go`, `uploadoperation` identity/journal | `backups` workflow admission/reconciliation, `passwordpolicy`, `serviceboundary` mandatory rejection | `service_client`, `service_download`, `snapshotfile`; caller transaction | `backups/http_provider_*`; immutable restore selection metadata |
| Connector management | `connectortargets` target/profile/runtime records | `connectormanagement` lifecycle admission and publication; `profileinput` schema/merge/validation | Caller-audited transaction, profile codec callbacks, connector management operations | `connectormanagement/presenters.go`, HTTP handlers; no decrypted secret projection |
| Database | `db/baselineschema` ordered definitions; installed SQLite schema | `db/migration_runner.go`, preflight/recovery and migration order | `db/db.go`, checkpoint/private-file publication | Schema version and query records; HTTP representation belongs to callers |
| Console | `console` per-session stream/capture state; `console/persistence` records | Session lock/admission, finality and history policy remain in `console`; `manualinput` observes input only | Authenticated runtime session/PTY and bounded stream writers | `console` display/redacted output; UI session owner |
| SSH transfers | Task/session state in connector `execution` | `execution` replacement, cleanup and uncertain transfer outcomes | Authenticated execution transport; `remotemetadata.Read` owns and drains its supplied stat session | Task progress/metadata; generic file-transfer history receives results |
| Connector target/action storage | `connectortargets` records; `actioncapacity` usage calculation | Root validates token/state and admission; child owns quota values/reservation arithmetic | Caller-owned SQL executor/transaction | `serialization.go` record decoding; management/action HTTP owners shape responses |
| Mail | Connector target/profile/action payload; `content` decoded message data | Connector read/write bounds, mailbox scope and untrusted-content warning | Connector IMAP/SMTP transport; bounded protocol operations | Mail action results/templates; central action/history delivery still mandatory |
| S3 | Connector target/profile/object/version identifiers | Connector scope, transfer budgets, pagination progress and mutation finality | Connector HTTP client and signing; `apiadapter` file-transfer integration | Connector result views; generic action/history delivery |
| Postgres | Connector target/profile and SQL/metadata payload | Connector readonly/query bounds and managed-role lifecycle; generic token permission outside connector | Connector database client through generic transport | Connector SQL result/template; role journal HTTP/UI projections |
| Gateway infrastructure | Captured workspace handle/components and scoped ports | Exclusive ownership, admission, delivery and required-audit composition | Workspace-owned factories, adapters and storage | Typed gateway capability facets; no duplicate connector kind dispatch |
| Project Vault | `projectvault/store.go`, items/bindings and workspace/session identity | Item mutation/quota, selected environment and session-finalization rules | Bound codec and caller transaction; runtime secret injection ports | Public metadata/session options; reveal is a separate authorized operation |

### High-Pressure Tests And UI/Tooling Owners

Tests do not own business policy. Their data column means fixtures/oracles, not
an alternate production database or permission implementation. Keep integration
proof at its caller; a child fixture must not import the parent that consumes it.

| Area | Data / fixtures | Policy under test | I/O / effects | Projection / assertions |
| --- | --- | --- | --- | --- |
| `api` tests | Owner-scoped request/DB fixtures | Actual composed route admission, workspace and required capabilities | Native disposable DB and HTTP fixtures where named | Public status/error/body contracts; do not replace with only mocks |
| `db/db_test.go` | Released encrypted schema and migration fixtures | Upgrade guards, ordering, relational invariants | Disposable SQLCipher files and transactions | Actual durable schema/rows; baseline contracts separately in `baseline_contract_test.go` |
| Architecture tests | Parsed source and supported dependency graphs | Import/layer, transport and connector-isolation constraints | Compiler/package graph tools and owned fixtures | Forbidden-edge/cycle diagnostics, including test imports; no cycle waiver |
| Database catalog tests | Owned catalog paths, encrypted files and recovery journals | Path/name validation, exclusive ownership, delete/move recovery | Disposable private directories and SQLCipher files | Actual catalog rows, surviving files and restart recovery state |
| Frontend shared template tests | Shared target/profile/session identities | Shared CRUD and request ownership | Controlled HTTP/deferred response fixtures | State transitions, exact identities and late-response rejection |
| Frontend Postgres tests | Typed database/role journal fixtures | Console/role ownership and consent | Template-local hooks plus `src/test/postgres` cross-boundary fixtures | Actual forms, result and recovery views; role tests stay in `role-lifecycle` |
| Frontend console/editor | Captured connector/profile/session and editor drafts | Per-request ownership and typed family capability capture | Console/editor hooks and controlled deferred requests | Actual controls/dialogs and stale-generation rejection |
| Frontend Mail/SSH templates | Connector-specific form/message/session values | Connector parsing and dialog ownership; token permission remains shared | Template-local model calls and native form serializers | Connector-local forms/operations; shared CRUD only for compatible models |
| Frontend console permission tests | Token/profile/action snapshots | Shared permission mode/expiry and selection rules | Controlled action/permission requests | Visible controls plus emitted exact payloads, not only snapshots |
| Frontend history | Public activity/transfer records | `use-history-transfer-download` owns request lifetime | Download hook and shared HTTP/download owners | `history-components.tsx` renders rows/dialogs |
| Frontend HTTP/retry | `lib/local-action-retry/entries.ts` durable client attempts | HTTP workspace/acknowledgement and retry ownership in `lib/api.ts` and retry owners | Fetch, bounded buffered/native downloads and browser storage | Shared gateway contracts/errors; never infer server mutation success from UI state |
| MCP tests | `test/support` public response/HTTP fixtures; `test/init` owned private files | Wire contracts, setup conflict and idempotency/unknown-result handling | Isolated HTTP and temporary-file tests | Exact tool schemas/responses; generated contract drift tests |
| Release tests | Source/version/notes/workflow fixtures | Exact release-source and publication sequencing contracts | Owned temporary Git/command fixtures, not public release operations | Version/notes/image/source assertions in feature subdirectories |
| Verification/maintenance tests | Policy/source/build fixtures | Ratchets, explicit floors, build/runtime and workflow isolation | Owned temp roots or child processes | Actual gate failures and diagnostics; no broad exemption to accommodate a new owner |
| Frontend scripts/E2E | Build/manifest and browser fixtures | Coverage/architecture budgets and browser workflow ownership | Bounded builds and separate mocked/real backend browser runners | Budget failures and visible workflow assertions; mocked runs do not establish service conformance |

The same area can have both a package warning and a single-file warning. For
example, mail root and `imap.go`, or target storage and `action_records.go`, are
one responsibility map with two capacity constraints. Re-run the checker to
find new warning owners; apply the same four-column analysis instead of raising
the ceiling or moving unrelated lines into a new directory.

## Changes And Proof

For every extraction, record the changed production/test files and packages,
before/after parent counts, new child limits/floors and combined source delta.
Count deleted/moved paths explicitly; do not present a smaller parent as a net
reduction. These metrics describe source layout, not measured contributor speed.

Pin the original behavior before moving it. Preserve caller-owned transaction,
audit and authorization semantics; then run the child contracts, complete parent
and adjacent suites, and the import graph including tests. Literal SQL or AST
comparison can prove an unchanged algorithm, but cannot replace native outcome
tests. Required examples include exact identities, byte rather than character
counts, actual rollback, stale authority, finality and canceled/late replies.

Use the existing maintenance/coverage/function/import/duplicate-code gates.
Do not add a package exception, lower a floor, grant a new dependency privilege,
or bypass a test cycle as a side effect of extraction. Verify the final immutable
source again after amend/rebase; earlier passing runs belong to their own source.

## DRY Without Erasing Boundaries

A one-line method is a candidate, not proof of duplication. A read-only facet
can intentionally hide mutations on its underlying store; an admission adapter
can pin authority/lifetime; a native serializer can keep connector-specific
types out of the generic pipeline. Removing these can expose capabilities.

Measure direct forwarding candidates and repeated mapping branches, then trace
their actual callers and return contracts. Consolidate duplicated decisions,
not merely similarly named fields. Keep transport-independent pure rules in one
owner, and connector implementation details in that connector's directory.
Do not replace explicit security contracts with reflection or another universal
facade just to reduce file count. Scoped source review and regression evidence
must state what was inspected; they do not certify all forwarding as harmless.
