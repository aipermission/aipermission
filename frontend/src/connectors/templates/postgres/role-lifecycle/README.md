# Managed Role Lifecycle UI

This feature owns journal decoding, keyset history navigation, receipt identity
checks, explicit operator consent and credential-backed recovery decisions.
`role-history-loader` is the lazy entry point used by the Postgres operations
template. Provisioning and backup/restore keep their separate owners.

Presence and cleanup decisions share one request owner and one consent form.
They retain exact entry generation, target, workspace and selected admin
ownership through dispatch and mandatory history reload. Form consent is
single-use; an old handler cannot regain authority after selection/evidence
changes, consent withdrawal, closing/reopening or unmounting. Unknown replies
never cause automatic mutation retries.

Production code and owner-local tests stay together in this feature directory.
The cross-boundary HTTP/hook tests and shared fixtures live in `src/test/postgres`.
The test manifest and protected behavior mappings lock the actual state and consent
owners, not just disabled-button rendering. These component tests do not replace
the backend's required native journal and PostgreSQL conformance tests.
