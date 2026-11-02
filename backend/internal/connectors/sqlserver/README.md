# SQL Server Connector

This package owns SQL authentication, direct TDS/TCP transport, TLS settings,
T-SQL policy, metadata and bounded tabular results. It does not persist targets,
credentials, permissions, sessions, history or audit records.

The first version is read-only: `get_schemas`, `get_tables`, `describe_table`,
`get_metadata`, and `query_readonly`. Metadata is limited to the configured
database and the login's visibility. Query inputs accept a single SELECT or
WITH SELECT and a small reviewed set of built-in functions and scalar casts.
Unsupported syntax fails closed. Stored procedures and write SQL are not
available even under Always permission.

Use an actual least-privilege read-only database login. SQL Server does not
provide Postgres-style read-only transactions; ApplicationIntent=ReadOnly is
not a security boundary and is not used as one. Read validation cannot certify
the behavior of operator-created views, synonyms, CLR types, or other
database-side objects. Do not grant untrusted routines, external access, linked
servers or administrative roles to AI-facing profiles.

Execution uses a 20-second context deadline, a 5-second lock timeout, server
ROWCOUNT (requested limit plus one for truncation detection), TEXTSIZE 131072,
1000 returned query rows, 5000 metadata rows, 256 columns, 64000 bytes per cell
and 500000 bytes per projected result. Output truncation is explicit and is not
proof that a metadata listing is complete. Results preserve duplicate column
names, exact large integers, decimal strings, binary hex and temporal offsets.
Timezone-free date/time types do not gain a timezone during serialization.

User SQL requires SQL Server 2012+ first-result description on the same connection
before execution, with ROWCOUNT disabled during metadata inspection. XML,
sql_variant, CLR/spatial and unknown result types fail closed; explicitly CAST
to a supported scalar type. `max_length_bytes` reports catalog byte lengths.
This preflight assumes trusted stable database objects: concurrent DDL or a
hostile server can invalidate metadata, and projected output limits are not an
unconditional driver/process heap cap. TEXTSIZE does not itself bound XML/CLR.
Catalog actions enumerate the target database, while permitted SQL may still
read server metadata visible to the login under SQL Server grants.

See [dependencies and transport policy](DEPENDENCIES.md). Protocol tests use
only owned disposable SQL Server fixtures, never operator databases.
