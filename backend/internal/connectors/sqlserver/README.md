# SQL Server Connector

This package owns SQL authentication, direct TDS/TCP transport, TLS settings,
T-SQL policy, metadata and bounded tabular results. It does not persist targets,
credentials, permissions, sessions, history or audit records.

The read actions are `get_schemas`, `get_tables`, `describe_table`,
`get_metadata`, and `query_readonly`. Metadata is limited to the configured
database and the login's visibility. `query_readonly` accepts a single SELECT
or WITH SELECT and a small reviewed set of built-in functions and scalar casts.
Unsupported syntax fails closed. Stored procedures and write SQL remain
unavailable through `query_readonly`, even under Always permission.

Use an actual least-privilege read-only database login. SQL Server does not
provide Postgres-style read-only transactions; ApplicationIntent=ReadOnly is
not a security boundary and is not used as one. Read validation cannot certify
the behavior of operator-created views, synonyms, CLR types, or other
database-side objects. Do not grant untrusted routines, external access, linked
servers or administrative roles to AI-facing profiles.

Read execution uses a 20-second context deadline, a 5-second lock timeout, server
ROWCOUNT (requested limit plus one for truncation detection), TEXTSIZE 131072,
1000 returned query rows, 5000 metadata rows, 256 columns, 64000 bytes per cell
and 500000 bytes per projected result. Output truncation is explicit and is not
proof that a metadata listing is complete. Results preserve duplicate column
names, exact large integers, decimal strings, binary hex and temporal offsets.
Timezone-free date/time types do not gain a timezone during serialization.

`query_readonly` requires SQL Server 2012+ first-result description on the same connection
before execution, with ROWCOUNT disabled during metadata inspection. XML,
sql_variant, CLR/spatial and unknown result types fail closed; explicitly CAST
to a supported scalar type. `max_length_bytes` reports catalog byte lengths.
This preflight assumes trusted stable database objects: concurrent DDL or a
hostile server can invalidate metadata, and projected output limits are not an
unconditional driver/process heap cap. TEXTSIZE does not itself bound XML/CLR.
Catalog actions enumerate the target database, while permitted SQL may still
read server metadata visible to the login under SQL Server grants.

## Explicit SQL Execution

`execute_sql` is a separate, always-write, non-idempotent action, including when
the script contains only SELECT. It accepts DML, DDL/schema migrations,
permissions, EXEC, stored procedure definitions and calls, and other statements
supported by the pinned T-SQL grammar. It is not a current-database sandbox:
routines, dynamic SQL and external effects run under the SQL Server login's
grants. Choose separate profiles and approval policies appropriate to this
authority. The original SQL, including comments, whitespace and Unicode, is the
canonical approval payload, not a rewritten read/write classification.

Scripts have a 64000-byte UTF-8 limit, at most 32 nonempty batches, 8192 visible
tokens, 32 parenthesis levels, 16 comment levels and 256 parser rule levels.
GO must occupy its own line, optionally surrounded by whitespace or comments.
GO repeat counts (including GO 1), leading/repeated empty GO batches, SQLCMD
directives (`:r`, `:connect`, `:setvar`, `!!`) and unquoted SQLCMD substitutions
are rejected. GO inside a literal, quoted identifier or comment is not a batch
separator. Dispatched chunks are exact contiguous approval-source byte slices.
A trailing comment-only chunk is dispatched separately and counts toward the
32-batch limit; a whitespace-only tail after GO is omitted. Client substitutions
inside SQL literals are not expanded. Parsing is syntax validation, not a
statement/function allowlist, authorization check or server compatibility proof.

Execution re-prepares the action and validates runtime, target, profile, action
identity and risk before secret resolution. A dedicated native connection is
authenticated and initialized before dispatch; no pool retries or session
resets can replay SQL or discard transaction state between batches. The selected
1-300 second timeout bounds the entire action, including authentication, all
batches and final transaction inspection. It also configures native socket and
lock timeouts without changing read timeouts. Connector write initialization
clears the driver's inherited SessionInitSQL, so native Connect only marks the
fresh session for reset; the one explicit selected-timeout setup consumes that
flag before any user batch. Read initialization is unchanged. Write setup
never sets ROWCOUNT. TEXTSIZE 131072 initially bounds supported text/binary
LOBs, but scripts can change session settings; it is not an XML/CLR heap cap.

Transactions belong to the script. There is no automatic BEGIN/COMMIT wrapper
and no atomicity promise. Finish requires both `@@TRANCOUNT` and `XACT_STATE()`
to be zero on the same connection. An open/uncommittable transaction is rejected
and connection teardown rolls it back; earlier commits and external effects may
remain. Authentication/setup failures are ordinary pre-dispatch errors. Errors,
timeouts, cancellation, unsupported results or failed final acknowledgement
after dispatch return `outcome_unknown` with `retry_safe: false`; reconcile
external state before any fresh attempt.

The shared collector bounds retained output, not execution. Native iteration
continues through all results and batches after row, column, result-set, notice
or output retention limits. Discarded rows skip connector normalization. The
pinned driver's `sqlexp.ReturnMessage` queue is drained concurrently so PRINT
and DONE messages cannot block row consumption. `server_done_count` commands
retain server-reported counts as decimal strings. These may include SELECT
counts, omit counts under NOCOUNT, or represent nested routine statements;
they are not an inferred affected-row total or a commit receipt.

### Driver Prefetch Limitation

All write-result columns are checked before connector `Next`, including after
output truncation. XML, sql_variant, UDT/CLR and unknown types abort with an
uncertain outcome. However, the pinned driver asynchronously decodes result
tokens and buffers rows before the consumer checks column metadata. These
checks **do not** provide a universal driver/native heap guarantee. The pinned
driver validates advertised PLP lengths against its supported LOB maximum,
not the connector's output budget. First-result description cannot cover
arbitrary DDL, dynamic SQL and procedures with changing/multiple result sets.
The execution contract bounds retained/serialized output and rejects unsupported
types when metadata is available, assuming trusted database/schema/grants.
The existing single-SELECT read preflight remains unchanged. Mock tests establish
the connector's pre-Next checks, not an upstream allocation bound; no driver fork
or strict pre-materialization guarantee is required by this contract.

See [dependencies and transport policy](DEPENDENCIES.md). Protocol tests use
only owned disposable SQL Server fixtures, never operator databases.
