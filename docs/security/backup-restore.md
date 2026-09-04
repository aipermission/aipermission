# Backup And Import

Because aipermission is local-first, database portability should stay simple.

## Current Model

The Settings page Backup panel downloads the currently unlocked database file directly.

Extension:

```txt
.aipdb
```

This file is a SQLCipher encrypted SQLite database. There is no separate backup password; the security boundary is the database password that was active when the file was downloaded.

## Portable Vault Secret

SSH private key payloads are encrypted with the gateway vault secret. To make single-file export/import work, the gateway secret is also stored in the unlocked encrypted DB as `settings.gateway_secret`.

When a `.aipdb` file is imported on another machine, the backend:

1. Opens the SQLCipher database with the password provided by the user.
2. Reads the gateway secret from the DB.
3. Starts vault/store layers with that secret.

No separate `gateway.secret` file needs to be moved.

## Import Model

The unlock/setup screen Import Database flow:

1. The user selects a SQLCipher-encrypted `.aipdb` or `.db` file.
2. The user enters a database name.
3. The user enters the database password.
4. The browser uploads with `multipart/form-data`.
5. The backend streams the file to a temporary path.
6. The password is verified against the encrypted database.
7. Plain SQLite files are rejected instead of converted.
8. The backend stores valid encrypted imports as a named local database and unlocks it.

Import never overwrites an existing database file. If a requested name collides with an existing database, the backend creates a unique database id or rejects the write rather than replacing data.

Import is available while the backend is locked.

## Removed Export Formats

Older `.aipbackup` JSON export/restore endpoints are no longer registered in the public REST surface. The supported workflow is encrypted `.aipdb` download/import only.

Active user flow:

```txt
GET  /api/backup/download
POST /api/backup/import
```

Plain SQLite files, JSON/base64 database payloads, and `.aipbackup` files are not imported by the current UI flow. New backups should use `.aipdb`, and imports should use `multipart/form-data`.

## Self-Hosted Remote Backup

Settings can store an optional AIPermission Backup service URL, public stream
metadata, and an encrypted service token. New providers are disabled by default.
Testing a provider verifies authentication and protocol compatibility without
enabling uploads.

Enabling a provider requires the current database password. AIPermission
verifies that password locally and applies a stronger remote-backup policy:
at least 18 characters, uppercase and lowercase letters, numbers, sufficient
character diversity, and rejection of common, repeated, sequential,
product-related, and database-name-derived patterns. The stronger gate matters
because theft of an encrypted remote backup permits offline password guessing.
Changing the password while remote backup remains active applies the same gate.

The service token is encrypted with the local gateway vault and is never
returned by list/detail responses. Archiving a provider clears its encrypted
secret locally. The provider service receives only its own bearer token,
already-encrypted `.aipdb` bytes, and bounded backup metadata. It never receives
the database password, gateway vault key, decrypted contents, MCP tokens,
connector credentials, SSH keys, or permission rules.
Responses that reflect the token, a normalized or encoded token representation,
or the derived bearer value in metadata, headers, or errors are rejected before
they can be persisted, audited, or returned to the browser.

`Upload backup` creates a temporary consistent SQLCipher snapshot and uploads it
unchanged as a new immutable version. The stable random workspace UUID stored
inside the encrypted database is used as the stream id, so two unrelated local
databases with the same display name remain isolated. Local metadata records the
remote version id, filename, size, checksum, source installation, and timestamps.
Download and restore re-check remote metadata and verify the received size and SHA-256.
Restore then validates the user-provided SQLCipher password and schema and
installs a new local database without overwriting the current one.

First-run restore is available while no local database is unlocked. The service
URL and token are accepted only for the list or restore request and are never
persisted. The selected stream identity cannot be changed during restore, while
the user may choose a distinct local display name for the restored copy.

Prune is an authenticated, explicit, stream-scoped destructive operation. The
operator chooses how many newest versions to retain, with a minimum of one.
Metadata deletion is transactional and remote blob cleanup is durably queued so
an interrupted cleanup resumes instead of exposing a partially pruned listing.

Automatic retention uses the same stream boundary and final-version guarantee.
The local UI can preview exact retain/delete counts before changing policy.
Saving with `apply_now` authorizes immediate deletion; saving without it defers
enforcement until a later explicit successful upload. Quota and pending-delete
values come from the backup service and do not expose the database password or
SQLCipher key. A compromised or incompatible backup service can still delete
or replace encrypted blobs, so keep another recovery copy for important data
and treat service-protocol validation failures as hard errors.

This does not make AIPermission a remote gateway. The separate backup service is
a passive encrypted-blob store with no command execution, connector access,
accounts, team model, or control plane. Continuous two-way sync and background
uploads are intentionally outside the current model; every operation remains an
explicit local user action.

See the [AIPermission Backup guide](../providers/aipermission-backup.md) for
deployment and restore instructions.

## Recovery Verification Order

Local import and self-hosted restore use the same fail-closed installation
boundary:

1. Stream the candidate into a temporary local file.
2. For remote versions, verify the declared size and SHA-256 before opening it.
3. Open it with the user-provided SQLCipher database password.
4. Require a supported schema and the embedded gateway secret.
5. Install it under a new local database identity without overwriting an
   existing database.
6. Start the gateway from the installed copy only after every check succeeds.

A failed download, checksum, password, schema, or gateway-secret check leaves
the active database untouched. Temporary candidates are not exposed as local
databases and are cleaned at the import boundary. Local import, provider
download, and first-run remote restore files are staged under the same private
temporary directory; abandoned files older than 24 hours are scavenged during
startup. Database rename/move writes a durable local journal before moving the
SQLCipher file, WAL/SHM sidecars, or migration snapshots. Startup rolls an
incomplete journal back to its source and preserves a durably completed move.

The database password and gateway secret serve different purposes. The password
opens the SQLCipher file; the gateway secret decrypts connector credentials
inside it. Current backups embed the gateway secret in the encrypted database.
For an older source that does not, preserve the original secret and follow the
[database migration recovery guide](../setup/database-migration.md). Never
substitute a newly generated gateway secret for an old one.

Treat service unavailability, protocol mismatch, size mismatch, and checksum
mismatch as hard failures. Correct the provider or network state and retry; do
not bypass verification. Review the local audit trail after a restore attempt
when diagnosing an unexpected result.

Release recovery drills cover local encrypted snapshot/import/restart,
self-hosted download/checksum/import/restart, and legacy migration retry after a
missing or incorrect gateway-secret condition. They use deterministic local
fixtures and do not make release checks depend on an external backup provider.

## Security Notes

- `.aipdb` files are sensitive but should be SQLCipher encrypted.
- The database password is not stored next to the file.
- Backups created before a password change open with the old password.
- Import must fail with the wrong database password.
- Private keys must not appear in API responses after import.
- Backup requires an unlocked database.
- Remote backup requires the stronger database-password policy before it can be
  enabled.
- First-run remote restore keeps service credentials transient and validates the
  encrypted database locally before installation.
- Mail action history can include bounded incoming bodies, outgoing drafts,
  recipients, subjects, and approval previews. These records remain encrypted
  but travel with `.aipdb` backups; configure finite history retention for
  sensitive mailboxes.
