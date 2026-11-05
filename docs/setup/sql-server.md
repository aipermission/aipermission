# SQL Server Connector

Connect directly from the local AIPermission gateway to Microsoft SQL Server
over TCP. The database may run on Windows: neither SSH nor a Windows-side
AIPermission agent is needed. The gateway itself remains local-only.

## Connection

1. Make the SQL Server TCP endpoint reachable from AIPermission's Docker
   network, directly or through your workstation's private network/VPN.
2. Enable TCP/IP for the SQL Server instance and use its actual listening
   port. `1433` is a default, not a guarantee. For named instances, supply a
   hostname and explicit port instead of `SERVER\INSTANCE`; SQL Browser
   discovery is not used.
3. Permit only the necessary source addresses through the Windows/network
   firewall. Do not expose the database publicly just to use this connector.
4. In **Connectors > Add Connector > SQL Server**, enter the host, port and
   exact database name. Supply a SQL login username and password for the
   default credential profile, then save and test the connection.

SQL authentication must be enabled for the instance. Windows integrated
authentication, Kerberos/NTLM and Entra ID are outside this initial scope.
The password stays in the encrypted credential profile. Additional profiles
use the same generic Credentials page and target/profile permission model.
Editing without a new password preserves the existing secret; changing the
username requires a password. Deleting a local profile does not drop the
remote database user.

There is no need to know the server version before saving. A successful
connection test reports `server_version`. The disposable compatibility suite
qualifies SQL Server 2022 on Linux; it does not certify every Windows release
or old server's TLS support. Failures must be investigated, not resolved by
silently weakening encryption.
Read-only queries require SQL Server 2012 or newer and permission to describe
their first result through `sys.dm_exec_describe_first_result_set`. A failed
description is an error, never a reason to execute without inspection.

## TLS

**Verify full** is the default. It requires TLS 1.2 or newer and verifies the
server certificate and hostname. For a private CA, paste its public PEM
certificate into **Trusted CA certificate**. When connecting to an IP address,
**TLS server name** may name the certificate's actual DNS identity. These
fields accept neither private keys nor paths into the gateway filesystem.

An untrusted self-signed server certificate is rejected. Export/verify the
public certificate or issuing CA through a trusted administration channel
before trusting it. The connector never retries with certificate checks off.
**Disable (unencrypted)** is an explicit, risky operator choice: traffic and
credentials may be exposed. It is not a workaround recommended for an
untrusted network. TLS-only fields are omitted when this mode is saved.

Connection tests distinguish invalid configuration, network failures,
certificate/TLS failures, rejected credentials and denied database access.
A Vault/secret delivery failure is not classified as a bad SQL password.

## Console And MCP

The console reuses the shared SQL editor, schema/table/column browser,
autocomplete, recent queries, Ctrl+Enter, JSON/result views and exports.
New Session/End Session are local presentation sessions, not a reusable
transaction or a pool of persistent database connections.
Autocomplete matches SQL Server identifiers exactly as a conservative UI
policy; this is not a claim about the database's collation. Use the spelling
shown in the schema browser. Column storage lengths are `max_length_bytes`
(including SQL Server's `-1` marker for MAX types), not character counts.

The same actions are discoverable through `get_connector_actions`:

| Action | Purpose |
| --- | --- |
| `get_schemas` | List visible schemas in the configured database. |
| `get_tables` | List visible tables and views, optionally by exact schema. |
| `describe_table` | Inspect ordered columns; omitted schema means `dbo`. |
| `get_metadata` | Load bounded table/column metadata for autocomplete. |
| `query_readonly` | Execute one reviewed SELECT or WITH SELECT statement. |
| `execute_sql` | Explicit write/admin execution for data changes, schema migrations and routines. |

Example action input:

```json
{
  "target_ref": "sqlserver:14:9",
  "action_name": "query_readonly",
  "input": { "sql": "SELECT TOP (25) * FROM [dbo].[orders] ORDER BY id DESC", "max_rows": 25 },
  "reason": "Inspect recent order metadata",
  "idempotency_key": "orders-metadata-inspection-1"
}
```

Use the target reference returned by discovery, not this example's IDs. Token
project visibility, Disabled/Blocked/Prompt/Always rules, approval drift,
history, audit and redaction are the existing generic pipeline. No new MCP
tool family, connector-specific request table or permission system is added.

## Read Boundary

Use a dedicated login with only the required SELECT permissions. Avoid `sa`,
`db_owner`, routine execution, external access and linked-server privileges.
SQL Server does not supply Postgres-style read-only transactions;
`ApplicationIntent=ReadOnly` is not a write-prevention mechanism.

The `query_readonly` action rejects writes, stored procedures/EXEC, multi-statement batches,
SELECT INTO, sequence consumption, table functions, linked/cross-database
table references, hints and unreviewed functions/syntax. Always permission
does not unlock those operations on the read action. Use the separately
authorized `execute_sql` action when writes, migrations or routines are needed;
see [Explicit SQL Execution](sql-execution.md) for scope, result and retry rules.
Operator-defined views, synonyms and CLR
objects still require database-side privilege review; SQL validation cannot
certify their implementation. Backup/restore and managed-user creation are
not included.

The catalog actions enumerate the configured database. This is not a promise
that SQL hides all server metadata: allowed built-ins and system views may
expose information visible to the login under SQL Server's own grants. Review
those grants separately from connector target selection.

Queries have a 20-second deadline, a 5-second lock timeout, up to 1000 returned
rows (100 by default), 256 columns, 64000 bytes per cell and 500000 bytes of
projected output. The connection also sets server ROWCOUNT and TEXTSIZE limits.
Metadata is capped at 5000 column rows; a truncated listing is not complete.
Large integers and decimals retain exact string representations where needed.
Binary values use hexadecimal text. Query data can itself contain sensitive
information, and redaction remains best-effort.

Before executing user SQL, the connector describes that exact compiled query
on the same connection. XML, `sql_variant`, CLR/spatial and unknown result types
are rejected; explicitly CAST them to a supported scalar type when needed.
This avoids unbounded XML/CLR buffering on stable schemas and avoids guessing
variant subtypes that the driver does not preserve. DATE and TIME are date-only
and time-only text; DATETIME/SMALLDATETIME/DATETIME2 have no invented timezone.
Only DATETIMEOFFSET includes its offset. REAL/FLOAT remain approximate values.

TEXTSIZE limits supported text/binary values, not XML or arbitrary CLR objects.
Projected output limits are not a universal process-memory ceiling. The driver
buffers values before the output builder, and schema changes between describe
and execution or a hostile server can invalidate preflight assumptions. Use
trusted database objects and least privilege; same-connection inspection does
not lock the schema or certify custom conversions.
