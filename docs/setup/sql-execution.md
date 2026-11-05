# Explicit SQL Execution

Postgres and SQL Server have two separate action authorities:

| Action | Authority |
| --- | --- |
| `query_readonly` | The connector's existing validated, bounded read policy. |
| `execute_sql` | Explicit SQL execution: data changes, schema migrations and routines, including potentially destructive operations. |

The shared SQL console starts in **Read-only** mode. **Execute SQL** is available
only when the connector declares that capability. Choosing it does not change
the read action's policy. ClickHouse remains read-only.

Both actions use the same target/profile, permission, approval, encrypted
request, history, audit and response-redaction pipeline. No separate MCP tool,
SQL permission store or connector-specific core route is introduced. The action
catalog exposes `execute_sql` automatically. Existing read permission does not
grant it; explicitly configure its token rule before an AI agent uses it.
Prefer Prompt until the workflow is understood. Always deliberately permits
the same broad SQL execution without an approval dialog, not a restricted
subset of statements.

## Authority And Credentials

`execute_sql` is always a write action, even when the script contains only
SELECT. It is not a SQL sandbox and does not infer the operator's intent from
keywords. Database grants determine whether routines can modify other schemas,
databases, permissions or external state. Review triggers, ownership chains,
security-definer routines, linked servers and privileged extensions separately.
An allowed target is not proof that an administrative login is confined to its
configured database. Prefer a separate, narrowly granted execution profile;
retain a read-only profile for ordinary inspections.

The full script is approval material. Do not embed passwords or reusable keys
in SQL text: requests and results are retained in encrypted history and may
contain business data. Redaction is best-effort and cannot certify arbitrary
database output as secret-free. Credentials still belong in credential profiles.

## Execution Semantics

One action opens one fresh database connection. All batches inside that action
share the connection, including session settings and temporary objects. A
console New Session is presentation state, not a persistent transaction across
separate executions.

- Postgres sends the complete script through the native simple-query protocol.
  Statement lists, dollar-quoted routine bodies, `DO` and `CALL` are parsed by
  Postgres, not split on semicolons by the gateway. `psql` client meta-commands
  and SQL Server `GO` are not supported.
  Client COPY transfers (`FROM STDIN`, `TO STDOUT`, replication COPY) are not
  supported. Their native response closes the connection promptly and reports
  `outcome_unknown`; no empty successful download or interactive input wait is
  presented. Reconcile any earlier committed work before another attempt.
- SQL Server uses its connector-owned pinned T-SQL parser for bounded `GO`
  separators. `GO` must be on its own line, without a repeat count. SQLCMD
  commands, shell escapes and substitutions are unsupported. Strings, quoted
  identifiers, comments and Unicode are not rewritten into another script.
  `EXEC` and procedure definitions execute under the profile's SQL grants.
  Statement errors and unsupported result types are recorded while the current
  batch is drained to its native end; later `GO` batches are not sent. A lost
  transport, cancellation or deadline can still interrupt the active batch.
  Unsupported result sets are omitted and marked truncated, not silently
  presented as complete; the action reports an uncertain outcome.
- The script controls its transactions. There is no blanket automatic COMMIT,
  whole-script rollback guarantee, or migration rollback tool. Earlier batches,
  explicit commits, routines and external effects may persist after a later
  failure. An open or failed transaction at the end is not successful completion;
  the action-scoped connection is cleaned up rather than kept for another call.
  Postgres normally wraps a complete multi-statement simple query in its own
  implicit transaction unless explicit transaction commands divide it; an error
  rolls that transaction back. Statement acknowledgements are not commit proofs.
- Postgres output settings may change inside the script. If the native decoder
  cannot interpret a text value (for example a non-default DateStyle or bytea
  format), the complete server text is retained with a notice and the script
  continues. Results in an unsupported client encoding or containing invalid
  UTF-8 are omitted with an explicit notice and truncation marker; they are not
  silently replaced with different characters. Subsequent commands still drain;
  results after restoring UTF8 may be retained normally.
- Returned-row limits never become UPDATE/DELETE limits. Output truncation
  does not silently stop the remaining script. The protocol is consumed until
  completion or the overall deadline; discarded output remains unretained.

Input SQL is limited to 64,000 UTF-8 bytes, with at most 32 SQL Server batches.
For SQL Server, this is an upper byte ceiling, not a guarantee that every script
below it can be parsed: the pinned grammar also limits visible tokens to 8,192,
parenthesis depth to 32, parser rule depth to 256, and adaptive parsing work to
500,000 token-stream operations within one second (or the earlier caller
deadline). SLL and any LL fallback share those limits. Fixed generated grammar
initialization happens once at connector construction, before user-script
parsing. Caller cancellation and
deadlines are still honored. A validation timeout sends no SQL and is reported
separately from a server execution timeout. Dense INSERT/UPDATE lists
can reach a token limit well below 64,000 bytes. Unsupported grammar constructs
fail validation before dispatch; divide the migration into separately reviewed
scripts instead of silently truncating it. Prefer explicit `EXEC name` for a
procedure; the native driver can classify a bare single identifier as an RPC.
`max_rows` defaults to 100 and is limited to 1,000 retained rows across all result
sets. `timeout_seconds` defaults to 30 and accepts 1 to 300. Results retain at most
32 sets, 256 columns per retained set, 64,000 bytes per cell and 500,000 bytes of
projected output; notices and command summaries have separate finite caps.
Row retention also accounts for the shared JSON node and depth limits, including
the surrounding result envelopes. Oversized arrays or deeply nested values
cause rows to be omitted with `truncated` set; remaining SQL still completes and
its command acknowledgements remain available.
Non-finite floating-point values, including native array elements, are
represented as complete text (`NaN`, `+Inf` or `-Inf`). Array structure and NULLs
remain intact; conversion alone does not mean the output was truncated.
The shared boundary checks the 500,000-byte output cap again after credential
redaction and exact-number projection. If that expansion exceeds the cap, the
output is withheld and the dispatched action reports `outcome_unknown`, never
an oversized successful result or an automatic replay.
Driver/server buffers are not a universal process-memory ceiling. Unsupported
native result types can cause an uncertain outcome after execution begins;
explicitly cast XML, variant or provider-specific values to supported scalars.
The deadline bounds the gateway's attempt, not a guarantee that the server has
stopped every statement or external effect. Cancellation still requires state
reconciliation before another write.

## Results And Retry Safety

Output includes `result_sets`, `commands`, `notices`, `row_count`,
`result_set_count`, `completed_batches` and `truncated`. The console can show
each retained result separately or the complete raw JSON.
`commands[].reported_rows` is exact decimal text from native command messages;
mixed SELECT/procedure/trigger counts must not be interpreted as the number
of uniquely modified business rows. `completed_batches` acknowledges protocol
completion, not a proof that every batch committed a durable change.

`execute_sql` is `non_idempotent`. The gateway's idempotency key deduplicates
the local submission; it does not make INSERT, migrations or procedures safe
to repeat externally. After a post-dispatch error, cancellation, timeout or
lost observation, the result is conservatively `outcome_unknown` with
`retry_safe:false`. Inspect history and independently reconcile database state
before a new external attempt. Never automatically replay the script with a
new key. Preserve the original target, action, input, reason and key while
recovering its recorded gateway result.

Example MCP call input, using a discovered target reference:

```json
{
  "target_ref": "sqlserver:14:9",
  "action_name": "execute_sql",
  "input": {
    "sql": "UPDATE dbo.orders SET reviewed = 1 WHERE id = 42;",
    "max_rows": 100,
    "timeout_seconds": 30
  },
  "reason": "Mark the operator-approved order as reviewed",
  "idempotency_key": "review-order-42-once"
}
```

Use the same generic `call_connector_action` for Postgres, substituting SQL
valid for that connector. Discover the action and credential profile first;
never copy sample target IDs into a real workflow.
