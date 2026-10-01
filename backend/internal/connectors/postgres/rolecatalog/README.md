# Managed Role Catalog Fence

This PostgreSQL-owned package is a remote identity guard, not a generic gateway
policy. Callers must retain the transaction returned by `Begin` through snapshot,
mutation and commit, alongside local workspace lifecycle exclusion and the
durable role journal. `VerifyRole` never treats an absent name as confirmation.

The fence uses `READ COMMITTED` and explicit `SHARE ROW EXCLUSIVE` locks on the
shared role, membership, database, tablespace, comment and dependency catalogs. `NOWAIT`
rejects conflicting held locks; unsupported providers or missing privileges
must fail closed. No advisory-lock-only or unlocked name-check fallback exists.
These are coarse cluster-wide locks appropriate only for short managed-role
transactions, not ordinary SQL queries, backup or browsing.

The source rationale is PostgreSQL 16's role commands using `RowExclusiveLock`
on `pg_authid`, shared comments writing `pg_shdescription` under that same lock
mode, and ownership/ACL dependency updates writing `pg_shdepend`. This rationale
is not a substitute for real two-connection concurrency and privilege tests.

- [Role commands](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/commands/user.c)
- [Shared comments](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/commands/comment.c)
- [Shared dependencies](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/catalog/pg_shdepend.c)
- [Explicit locking](https://www.postgresql.org/docs/16/explicit-locking.html)

The local authority snapshot is checked separately by the lifecycle caller.
Remote verification compares exact cluster, database, successor, role OID/name
and the full random creation marker without normalizing identifiers.

The dependency lock is **not** a frozen ownership/ACL snapshot. Some writers
release it before committing, and changes preserving the referenced-role set
need not acquire it. Cleanup must retain PostgreSQL's native object locks and
final dependency checks and reject/roll back failures atomically. Object-first
writers can also deadlock with cleanup after catalog admission; bounded mutation
waits and durable uncertainty are required. `NOWAIT` only bounds initial fence
acquisition, not all subsequent mutation waits.

## Lifecycle Ordering

`Provision` persists intent before creation, records the exact role OID before
COMMIT, and confirms only an acknowledged commit or a fresh fenced observation
of the complete remote identity. Its recovery path never repeats CREATE/grants.
An acknowledged rollback is recorded separately from a missing connection.

`Reconcile` is observation-only on PostgreSQL: it never repeats creation,
grants, reassignment, revocation or deletion. It rejects a stale local generation
or changed current target/admin authority before connecting. A bound provisioning
intent can become provisioned after the complete remote identity matches; a
cleanup intent can return to provisioned only when that same exact role still
exists. The fresh catalog fence is retained through the local journal write and
readback, not released between observation and confirmation. Missing roles,
unbound provisioning intents and mismatched identities remain unresolved.

Presence reconciliation assumes one continuous authoritative cluster history;
PITR, physical restores, divergent clones or privileged identity reconstruction
require operator investigation. It does not attest an acknowledged rollback,
unchanged grants/password, credential publication or a completed cleanup. The
current authority must come from fresh server-owned target/admin metadata under
workspace-exclusive admission, and the connection must use that same runtime.
SQLCipher publication and the remote transaction are not a distributed atomic
commit; backend termination can also release catalog locks independently of the
client's transaction object.

`Cleanup` verifies current durable evidence and plans within the same transaction.
It first drains earlier shared database/tablespace ownership updates using
complete `FOR UPDATE` scans. The subsequent scope check is a **separate** READ
COMMITTED statement: a CTE using the earlier snapshot would not be sufficient.
Shared ownership and other-database dependencies require operator reconciliation.
The scans have a five-second overall deadline and no LIMIT/SKIP LOCKED fallback.

Cleanup executes REASSIGN, explicit privilege REVOKE and final DROP ROLE, never
DROP OWNED. Surviving or concurrently added ownership/dependencies make DROP ROLE
fail and roll the transaction back rather than deleting data. Default privileges,
policies or grantor dependencies not handled by the bounded revocation inventory
also fail closed; unsupported dependencies are not silently removed. The plan is
bounded to 10,000 statements and 1 MiB of SQL. Both lifecycle operations have a
20-second dispatch deadline and detached five-second confirmation/rollback limits.

Cleanup COMMIT uncertainty cannot be resolved from role-name absence. It leaves
the durable cleanup intent unresolved. A confirmed full rollback restores the
provisioned state, while a fresh cleaned record can skip remote dispatch. Current
local authority must still be checked before using that terminal confirmation.

- [REVOKE semantics](https://www.postgresql.org/docs/16/sql-revoke.html)
- [REASSIGN OWNED scope](https://www.postgresql.org/docs/16/sql-reassign-owned.html)
- [Tablespace creation locks](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/commands/tablespace.c)
- [Catalog ownership changes](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/commands/alter.c)

Unit/fake tests establish ordering and failure contracts, not native SQL or
concurrency correctness. Required conformance fixtures execute real cleanup,
verify data preservation and column-only revocation, reject cross-database
ownership, and observe actual blocking before a prior tablespace writer commits,
rolls back, times out or is canceled. The cancellation case must finish before
the normal drain deadline and report its actual canceled cause. Fixture teardown
uses an independent connection, waits for canceled-connection cleanup and checks
that no fixture-owned catalog objects remain. These service tests must pass
before release.

The reconciliation conformance fixture also uses real SQLCipher persistence and
requires a cross-database catalog writer to time out both before the decision
write and during its fresh readback. Committed creation and rolled-back cleanup
retain exact identity; committed cleanup leaves an unresolved intent rather
than treating absence as evidence. These are native release prerequisites, not
claims established by the fake lifecycle tests.

The late current-database ownership fixture distinguishes native lock refusal
from a writer that actually commits after successful reassignment. Refusal proves
data preservation under that lock schedule only; it does not prove the late-commit
schedule ran. When that schedule is observed, final DROP ROLE must reject the
surviving ownership and rollback must restore the earlier reassignment. The
ordinary cleanup fixture also checks another non-superuser's independent SELECT
grant survives. These are required native assertions, not fake-driver coverage.

Required lost-reply fixtures replace only the transport: pgx sends the production
catalog SQL to PostgreSQL, while a protocol proxy withholds the server's COMMIT
completion and idle ReadyForQuery messages. They inspect durable intent before
dispatch, verify the actual committed remote state, reopen SQLCipher, and reject
blind replay or cleanup. Explicit creation reconciliation remains observation-only;
cleanup name absence leaves its intent unresolved. Synthetic error-returning
transactions cannot substitute for these wire-level assertions.
