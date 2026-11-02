# SQL Server Dependencies

The connector uses Microsoft's `github.com/microsoft/go-mssqldb` driver at the
exact version in `go.mod`. Its BSD-3-Clause license and the named license files
of its dependencies are recorded in the reviewed dependency inventory.

Use `NewConnectorConfig`, not an interpolated DSN. The connector supplies the
reviewed generic TCP transport and an explicit port. SQL Browser discovery,
alternate protocols, Windows integrated authentication, connection-string
options, and redirects to a different endpoint are not supported.

TLS is required and certificate/hostname verified by default (TLS 1.2 minimum).
Public PEM CA certificates may be supplied for private certificate authorities.
Disabling TLS is an explicit operator choice; there is no automatic insecure
fallback or `TrustServerCertificate` mode. No custom CA file path is accepted.
