# ADR 0007: Transactional Audit Outbox

Status: accepted

## Context

AIPermission historically recorded security-relevant local mutations and
connector activity directly in `audit_logs`. That implementation had two
non-atomic patterns:

- most handlers commit a mutation and then write a best-effort audit row;
- selected Vault and permission paths write a required `requested` event before
  attempting the mutation.

The first pattern can commit state without its audit event. The second can keep
a request event for a mutation that never committed. Process-local degraded
health makes write failures visible, but it resets after restart and cannot
repair a missing event.

AIPermission is a local-only, single-user application backed by one SQLCipher
database. It does not need an external queue or distributed transaction system.
It does need a durable boundary between local state changes and their audit
record.

## Decision

AIPermission will use a transactional audit outbox for security-sensitive local
mutations.

The application service that owns a mutation will:

1. validate the request and build a canonical, already-redacted audit event;
2. begin one SQL transaction;
3. apply the domain mutation through transaction-aware stores;
4. append the event to `audit_outbox` through the same transaction;
5. commit both changes together.

`audit_outbox` is the durable delivery record. `audit_logs` remains the
searchable read projection used by the UI and REST API. A local dispatcher
projects outbox events into `audit_logs` idempotently and records durable
delivery state.

Connector implementations do not write either table. Shared gateway
application services continue to own permission, approval, history, and audit
behavior.

## Canonical Event

Each outbox event has a stable random event id and bounded canonical metadata:

- event id and event version;
- actor type and optional token id;
- project, runtime, connector, target, profile, and action-request references;
- action name and lifecycle phase;
- redacted JSON payload;
- occurrence and creation timestamps;
- delivery timestamp, attempt count, and last delivery error.

The event id is unique in both `audit_outbox` and `audit_logs`. Retrying a
projection after a crash must not create a duplicate audit row.

Payloads are redacted and size-bounded before they enter the transaction.
Decrypted credential values, private key material, Vault values, passphrases,
full connection strings, and unbounded output are forbidden in both the outbox
and the projection. The outbox is inside the encrypted database, but encryption
does not relax the credential boundary.

## Transaction Boundary

Stores that participate in an audited mutation accept a narrow database
executor implemented by both `*sql.DB` and `*sql.Tx`. The shared application
service, not the connector package, normally owns `BeginTx`, rollback, and
commit.

The intended shape is:

```go
type DBTX interface {
    ExecContext(context.Context, string, ...any) (sql.Result, error)
    QueryContext(context.Context, string, ...any) (*sql.Rows, error)
    QueryRowContext(context.Context, string, ...any) *sql.Row
}
```

Store constructors may accept a database or transaction through this interface.
Mutation methods must not start a nested transaction when an application
service already owns one.

Console session and file-transfer lifecycle state can also change in runtime
goroutines that do not originate in one HTTP application service. Narrow
database triggers cover only their create, status, queue, and archive-ready
transitions. They append bounded identifier/status metadata to the same outbox
transaction and deliberately ignore transcript, path, error, and progress
updates. Connector-specific tables and triggers are not permitted.

An outbox append failure rolls back the domain mutation. A domain mutation
failure rolls back the outbox append. Response serialization and non-durable UI
notifications remain outside the transaction.

## External Side Effects

SQLite cannot atomically commit an SSH command, database query, mail send,
object upload, or other remote side effect. Those operations use a durable
state machine rather than claiming end-to-end exactly-once execution:

1. persist a pending request, or a running request with a durable runtime owner
   and execution lease, together with its `requested` or `started` event;
2. immediately before connector dispatch, atomically claim dispatch for that
   owner while the lease and running state are still valid;
3. perform the bounded remote operation;
4. persist the terminal request state and `completed`, `failed`, `declined`,
   `canceled`, or `stale` event in a second transaction;
5. use connector-specific idempotency or compensation where it is available.

If the process stops after the dispatch claim and before terminal persistence,
recovery may know only that the outcome is uncertain. It must preserve that
uncertainty instead of inventing success or failure. A running request whose
lease expires before dispatch cannot later reach the connector; the dispatch
claim fails closed. Exactly-once remote execution is a non-goal.

## Dispatcher

One local dispatcher runs for each unlocked database runtime. It:

- wakes after commits and also polls on a bounded interval;
- drains events in ascending outbox id order in bounded batches;
- inserts the audit projection and marks the outbox row delivered in one SQL
  transaction;
- retries transient failures with bounded backoff;
- resumes pending delivery after unlock or process restart;
- never requires network access or an external broker.

The projection insert uses the event id as its idempotency key. A crash after
the insert but before delivery bookkeeping therefore converges safely on the
next attempt.

Delivered outbox rows may be removed only by an explicit retention policy.
Dead-letter rows are terminal delivery failures and may be removed by that same
explicit policy after its cutoff. Pending or retryable undelivered rows are
never retention candidates.

## Health And Operations

Audit health will become durable. `/api/status` should eventually report:

- pending event count;
- age of the oldest pending event;
- events with delivery attempts;
- latest durable delivery error and time;
- immediate process-local dispatcher diagnostics.

The current process-local failure count remains useful during migration, but it
is not the final source of truth. A healthy status must not hide an undelivered
backlog left by an earlier process.

## Implementation

The implemented boundary is deliberately layered:

1. `audit_outbox` and nullable unique `audit_logs.event_id` provide durable,
   idempotent delivery.
2. Shared audited-mutation helpers couple local domain changes and events.
3. Request stores couple hidden/background lifecycle transitions through
   transaction-aware hooks.
4. Runtime-owned console and transfer transitions use the narrow triggers
   described above.
5. Read observations and external side-effect telemetry may still use a
   best-effort audit event. They do not describe a committed local mutation and
   are not presented as an atomic guarantee. When an observation accompanies a
   trigger-owned durable lifecycle event, its action name ends in `_observed`
   so operators can distinguish telemetry from the committed transition.

Existing `audit_logs` rows remain valid and have a null event id. The migration
does not rewrite or synthesize historical events.

## Verification Requirements

The implementation is not complete until tests prove:

- mutation rollback also removes its outbox event;
- a successful commit contains both mutation and event;
- no credential plaintext enters outbox or audit projection storage;
- dispatcher retries are idempotent across crash points;
- startup/unlock drains an existing backlog;
- concurrent writers do not lose events;
- external-side-effect recovery preserves uncertain outcomes;
- retention never deletes undelivered events;
- durable degraded health survives process restart.

## Consequences

- Security-sensitive local mutations gain an atomic durable audit intent.
- Audit browsing remains responsive and searchable through `audit_logs`.
- Store transaction boundaries become more explicit and reusable.
- The gateway gains a dispatcher and migration complexity that must be kept
  local, bounded, observable, and well tested.
- Observation events remain intentionally distinct from mutation guarantees.

## Non-Goals

- an external message broker or hosted audit service;
- multi-user or compliance-grade remote log shipping;
- exactly-once execution of remote connector side effects;
- storing raw credentials or secrets because the database is encrypted;
- connector-specific audit pipelines.

## Related

- [Credential Boundary](../security/credential-boundary.md)
- [MCP Permission Flow](../architecture/mcp-permission-flow.md)
- [ADR 0004: SQLCipher For Local Databases](0004-sqlcipher-choice.md)
