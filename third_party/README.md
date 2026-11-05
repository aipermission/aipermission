# Pinned Dependency Patches

`go-mssqldb/` contains the production Go sources and license from Microsoft
go-mssqldb v1.11.2, commit `25101949abc460a9fe03b60c5cca87c1a5529a8c`.
Examples, upstream integration tests and their fixtures are not vendored.
The original module path and required version remain in `backend/go.mod`.

The local patch makes connection shutdown abort blocked token/message delivery,
close transport, join the native response reader, and only then return the TDS
buffer to its pool. Connection shutdown is idempotent. Ordinary query cancellation
retains its existing attention/completion semantics. It adds no SQL retry.

The unpatched driver's Rows/Rowsq close and transport close do not expose a
supported reader-join guarantee before Conn.Close returns its buffer. Closing
only a caller-owned socket and deliberately skipping Conn.Close is not a safe
substitute: token/message delivery can remain blocked and connection buffers
would remain unreleased. The narrow lifecycle patch supplies an explicit join;
it is not a claim that no other ownership design could satisfy the same tests.

`go-mssqldb-upstream.json` records every upstream source digest and the explicitly
patched files. The dependency check rejects undeclared source changes and
unlisted replacements. `go-mssqldb-close.patch` is the complete production diff
against that upstream snapshot; its digest is also recorded in the manifest.
This is third-party dependency source, not a connector
implementation or an exception for application source budgets.
The backend coverage oracle invokes this same Node verifier before treating a
repository-local external import as third-party source. A directory name or
module replacement alone never exempts application code from coverage.

Run the driver's lifecycle tests and the connector's native wire/real-service
tests after any update. Keep the upstream version, source checksums, license
inventory and vulnerability tracking visible. When upstream supplies an
equivalent tested lifecycle guarantee, remove this snapshot and replacement.

Additional local release qualifications (also required by CI):

```sh
(cd third_party/go-mssqldb && go test -race -count=3 .)
bash scripts/scan-go-vulnerabilities.sh
```

The scan checks both the patched application and a disposable modfile with the
upstream version restored: a local replacement must not hide upstream advisories.
