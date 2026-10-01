# Local Transaction Finality

`Run` owns one pinned local SQL transaction and releases the connection before
returning to its caller. Audit projection and connector resource readback must
run after it returns, not inside the callback against the same pool. The audited
mutation coordinator and scoped credential-resource creation use this owner;
connector domain code never receives its database or physical connection.

The callback must use the supplied transaction, must not commit/roll it back,
must pass the original caller context to its SQL operations, and must not
perform non-transactional external effects. Remote dispatch needs its own
durable journal and confirmation protocol.

Connection acquisition uses the caller context. The transaction itself starts
with cancellation detached so `database/sql` cannot asynchronously close the
pinned connection while this owner retires it. `Run` checks caller cancellation
before admitting the callback and again before COMMIT, performing an explicit
rollback on cancellation. BEGIN uses the local SQLCipher driver's deferred
transaction and bounded busy wait; detaching its context does not detach SQL
operations inside the callback. The owner synchronously finalizes the
transaction even when the caller has stopped waiting.

Failure finality is explicit:

- `NotCommitted`: no transaction callback began, or Rollback acknowledged the
  complete callback transaction. This can authorize compensation where the
  caller's domain contract supports it.
- `Unknown`: no usable finality or connection-retirement proof; no automatic
  compensation or publication readback is authorized.
- `UnknownWithSafeReadback`: finality is still unknown, but the owner has either
  acknowledged transaction completion or retired the uncertain physical
  connection. Fresh exact committed-state readback can be attempted.

A driver can leave an explicit transaction open after failed COMMIT while
`database/sql` considers `sql.Tx` finished. `Run` discards that pinned physical
connection through `sql.Conn.Raw` / `driver.ErrBadConn`; a deferred Tx.Rollback
alone is insufficient. Begin failure also discards the connection because a
driver may have started a transaction before returning the error. Callback panic
rolls back, retiring the connection if that cleanup cannot be acknowledged,
and propagates the original panic. A successful COMMIT is never retried.

Markers preserve original causes. Outer owner markers override nested callback
or driver markers; joined errors require proof in every branch. An unclassified
error or a missing database row never proves rollback. No rule is inferred from
driver message text, connector kind, runtime ID or credential-profile ID.

Fake-driver tests prove ownership, dispatch counts and retirement. Native
SQLCipher deferred-foreign-key fixtures additionally prove that uncommitted
profile/audit/resource rows cannot escape through readback and that a replacement
connection retains encrypted database access. Neither suite replaces the other.
