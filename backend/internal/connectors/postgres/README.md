# PostgreSQL Resource Limits

Connections reject incoming protocol message bodies larger than 4 MiB before
pgx decodes them. The cap applies to rows, metadata, authentication, notices and
errors, through both direct and transported connections. A row's complete wire
body must fit even when individual cells would later be truncated.

This input allocation limit is separate from the 500,000-byte projected output
budget and 64,000-byte cell limit. Ordinary large cells within the wire cap still
use output truncation. Protected read actions retain read-only transactions,
the audited function allowlist and resolution policy, and their existing
20-second request/session statement timeouts, including pooler-compatible setup.

## Explicit SQL Execution

`execute_sql` is a separate `write`-risk, `non_idempotent` action. Its approval
preview and encrypted execution payload retain the full script, including
whitespace. It supports data writes, DDL/migrations, `CALL`, `DO`, dollar-quoted
routine bodies, and multiple statements. It does not run a shell or `psql`:
backslash directives and client-side include commands are not supported.

Each approved action authenticates a fresh pgx connection through the existing
Direct/Over SSH transport. The script is sent unchanged as one native simple
query, with no read-only wrapper, SQL splitter, implicit transaction wrapper,
or automatic retry. Database role privileges remain authoritative. Use
`query_readonly` with a dedicated read-only role for protected reads; approving
`execute_sql` grants the script all capabilities of the selected database role.

Scripts control transactions. A session must finish idle; an open or failed
transaction returns conservative `outcome_unknown` and cleanup attempts rollback
before closing the connection. Explicit commits, routine side effects, and
external effects cannot be undone by cleanup. Any error after script dispatch,
including SQL errors, cancellation, timeout, decoding errors, and lost replies,
is `outcome_unknown` with `retry_safe: false`. Reconcile database state before
submitting another attempt, even when a particular error suggests rollback.

Input is limited to 64,000 SQL bytes. `timeout_seconds` accepts 1..300 (default
30), sets the write session's statement timeout, and bounds the entire action
including connection setup and all results; an earlier caller deadline wins.
The script can change session settings, but cannot extend the action deadline.
Cleanup has its own bounded budget. Read-only defaults are not increased.

The shared SQL stream output contains `result_sets`, `commands` (command tags
and decimal-string `reported_rows`), `notices`, counts, truncation and limit
metadata, and `transaction_control: script_defined`. Retained output is bounded
to 1..1000 total rows (default 100), 32 result sets, 256 columns per result,
64,000-byte cells, and 500,000 serialized bytes. Retention limits, including
excess column counts, truncate output without stopping the script. Rows use the
existing Postgres value normalization, including lossless bounded bytea hex and UTC
timestamps. All results continue to be consumed after retention limits are
reached so later statements and their acknowledgements are not silently skipped.

Connector-local wire tests use in-memory protocol peers only, not real endpoints.
Real Postgres migration/procedure/transaction semantics still require a disposable
real-service qualification before release.
