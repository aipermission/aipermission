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

The complete `tsql_file` rule must parse without lexer/parser errors. Core does
not interpret SQL: the connector allowlists grammar rules, functions, scalar
types and local table identities. Unknown rules, methods, table functions,
hints, extra statements and mutations fail validation. The dispatched SQL is
the visible token projection, preserving complete literals and identifiers
while omitting comments. An AST printer or silently skipped suffix never
authorizes execution. Input, token, projected-text and nesting limits bound
parsing; malformed inputs produce validation errors, not escaping panics.

The shared regression corpus includes parser/server lexical differences and
non-SELECT statements. Real-service tests use a login that can modify only an
owned canary table, so DENY permissions cannot conceal a failed SQL policy.
Database permissions remain mandatory defense in depth; parser validation is
not a certification of user-defined views, synonyms or server implementations.

TLS is required and certificate/hostname verified by default (TLS 1.2 minimum).
Public PEM CA certificates may be supplied for private certificate authorities.
Disabling TLS is an explicit operator choice; there is no automatic insecure
fallback or `TrustServerCertificate` mode. No custom CA file path is accepted.
