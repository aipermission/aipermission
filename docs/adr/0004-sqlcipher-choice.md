# ADR 0004: SQLCipher For Local Databases

Status: accepted

## Context

AIPermission stores connector targets, credential profiles, token metadata,
audit data, command/action history, message queues, and vaulted secret payloads.
The database may be backed up and moved between machines.

## Decision

AIPermission uses SQLCipher for full local database encryption. Secret payloads
inside the database are also handled through the gateway vault layer.

The backend pins `github.com/SE-I-T-Digital/go-sqlcipher` at full commit
`8f19266d2b2782e348b49e04848c6830429c8174`. That wrapper embeds SQLCipher
4.16.0 and SQLite 3.53.1 and links its cryptographic provider to OpenSSL 3. The
backend image makes CGO explicit, installs the build headers and runtime library
explicitly, and pins both build and runtime base images by digest. Published
image SBOMs record the resolved operating-system package versions.

This wrapper was selected over the previous unmaintained SQLCipher 4.4.2
binding because it follows current `go-sqlite3` behavior, exercises Linux,
macOS, and Windows builds upstream, and tracks a substantially newer official
SQLCipher amalgamation. It does not publish tagged releases, so AIPermission
pins a full commit through a Go pseudo-version and treats any change to its
default branch as a review signal, never an automatic update.

Official SQLCipher 4.18.0 was most recently reviewed on 2026-08-26. The
selected Go wrapper currently embeds 4.16.0, so 4.18.0 is recorded as reviewed
rather than silently claimed as active. The scheduled native-dependency
freshness workflow fails when either reviewed upstream source advances or the
45-day advisory review window expires and requires a deliberate compatibility
review.

## Compatibility Contract

SQLCipher promises file compatibility within one major version when the same
database settings are used. AIPermission keeps the SQLCipher 4 defaults and a
4096-byte cipher page size. A synthetic encrypted fixture produced by the old
4.4.2 runtime is committed under `backend/internal/db/testdata`; tests verify
its checksum and cover open, wrong password, corruption, snapshot, rekey,
rename, WAL, and clean reopen behavior with the active runtime.

Runtime updates must not silently rewrite the only copy of a user's database.
This 4.4.2-to-4.16.0 update opens the existing SQLCipher 4 file directly and
does not perform a format conversion. Any future change that requires an
in-place migration must:

1. create an encrypted snapshot first
2. open and verify that snapshot independently
3. migrate a separate working copy
4. verify schema, integrity, and representative reads
5. retain a documented rollback path to the prior runtime and snapshot

If compatibility verification fails, startup must leave the source file
unchanged and report a recovery error instead of attempting a best-effort
rewrite.

## Consequences

- Database passwords are unrecoverable.
- `.aipdb` files are portable encrypted workspace backups.
- The vault layer is not a second independent security boundary if the database
  password is also compromised.
- REST and MCP responses must never return SSH private keys or database secrets.
- Native backend development requires OpenSSL 3 headers; container builds own
  this dependency explicitly.
- Wrapper and native runtime updates require fixture tests and human review.

## Related

- [Storage Encryption](../security/storage-encryption.md)
- [Credential Boundary](../security/credential-boundary.md)
- [Native Dependency Inventory](../security/native-dependencies.md)
