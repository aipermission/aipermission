# SQL Server Dependencies

The connector uses Microsoft's `github.com/microsoft/go-mssqldb` driver at the
exact version in `go.mod`. Its BSD-3-Clause license and the named license files
of its dependencies are recorded in the reviewed dependency inventory.

Use `NewConnectorConfig`, not an interpolated DSN. The connector supplies the
reviewed generic TCP transport and an explicit port. SQL Browser discovery,
alternate protocols, Windows integrated authentication, connection-string
options, and redirects to a different endpoint are not supported.

T-SQL parsing uses the pinned [Bytebase ANTLR T-SQL grammar](https://github.com/bytebase/parser/tree/main/tsql)
and the official Go ANTLR runtime. The module/runtime named license evidence
is recorded in the inventory; the grammar's embedded MIT notices are in its
`.g4` sources. No dependency-local runtime replacement is inherited.

The complete `tsql_file` rule must parse without lexer/parser errors. For
`query_readonly`, core does not interpret SQL: the connector allowlists grammar rules, functions, scalar
types and local table identities. Unknown rules, methods, table functions,
hints, extra statements and mutations fail validation. The dispatched SQL is
the visible token projection, preserving complete literals and identifiers
while omitting comments. An AST printer or silently skipped suffix never
authorizes execution. Input, token, projected-text and nesting limits bound
parsing; malformed inputs produce validation errors, not escaping panics.

`execute_sql` uses the same pinned grammar but **not** the read allowlist or
token projection. A bounded parse-only shadow masks comments and adapts escaped
double-quoted identifiers without changing rune positions. ANTLR token offsets
are mapped back to original UTF-8 byte offsets. GO tokens are accepted only as
standalone client separators without counts. Only separator bytes and a final
whitespace-only tail are omitted; trailing comments remain a separate source
slice. No AST printing, SQLCMD preprocessing, include files or substitutions
are used. All dispatched chunks are contiguous original slices. `FuzzExecutionSQL`
checks batch limits, exact byte provenance, malformed/deep syntax and repeat
rejection and is registered in the repository verification policy. The existing
read fuzz target is unchanged.

Writes use native `driver.Connector.Connect`, contextual statements and one
physical connection, not pooled `database/sql` execution. The connector calls
native Connect with SessionInitSQL cleared for execution only: Connect marks
the fresh session for reset, and the one explicit selected-timeout setup
consumes that flag. No user batch triggers another reset. Read SessionInitSQL
is unchanged. If Connect returns a connection alongside an error, execution
closes it before returning the pre-dispatch failure. The connector calls
`NamedValueChecker` for the pinned `github.com/golang-sql/sqlexp` ReturnMessage
API and omits that argument from SQL parameters. A bounded concurrent observer
drains notices/counts while native Rows.Next drains results. Counts are labeled
`server_done_count` because this API supplies neither a SQL command tag nor a
reliable DML-only count. Never derive affected rows from SELECT result lengths.
NOCOUNT and nested routines affect count visibility; PRINT is informational,
not a success or commit proof. There are no transport or statement retries.
An execution-only socket observer cancels the action with its transport error
on failed reads/writes or deadline setters, waking the message loop even before
the first message.
The existing context binder closes the socket; read actions keep their existing
transport behavior.

The pinned driver's `processSingleResponse` eagerly parses row tokens into a
buffered token channel (`startReading` currently allocates capacity five).
`Rowsq.Columns`/`ColumnTypeDatabaseTypeName` are consumer-side APIs, not a
pre-allocation hook. XML/UDT PLP decoders and sql_variant may allocate before
those APIs are consulted, including for later procedure/dynamic result sets.
The pinned driver validates advertised PLP lengths against its supported LOB
maximum before initial allocation, but this is not the connector's retained
output budget. TEXTSIZE and metadata checks do not provide a universal driver
heap guarantee. Execution rejects unsupported types when metadata is available
and bounds retained/serialized output under trusted database/schema/grants
assumptions. Arbitrary dynamic/procedure results cannot all be predescribed;
the existing single-SELECT read preflight remains unchanged. No dependency
fork or pre-materialization heap guarantee is part of this execution contract.

The shared regression corpus includes parser/server lexical differences and
non-SELECT statements. Read-policy tests use a login that can modify only an
owned canary table, so DENY permissions cannot conceal a failed SQL policy.
Execution tests use a separate, non-administrative fixture login with grants
for disposable objects inside the isolated conformance database.
Database permissions remain mandatory defense in depth; parser validation is
not a certification of user-defined views, synonyms or server implementations.

TLS is required and certificate/hostname verified by default (TLS 1.2 minimum).
Public PEM CA certificates may be supplied for private certificate authorities.
Disabling TLS is an explicit operator choice; there is no automatic insecure
fallback or `TrustServerCertificate` mode. No custom CA file path is accepted.
