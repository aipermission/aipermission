# PostgreSQL Resource Limits

Connections reject incoming protocol message bodies larger than 4 MiB before
pgx decodes them. The cap applies to rows, metadata, authentication, notices and
errors, through both direct and transported connections. A row's complete wire
body must fit even when individual cells would later be truncated.

This input allocation limit is separate from the 500,000-byte projected output
budget and 64,000-byte cell limit. Ordinary large cells within the wire cap still
use output truncation. Queries also retain row limits, read-only transactions
and session-level statement timeouts, including pooler-compatible setup.
