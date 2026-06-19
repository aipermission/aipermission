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

## Remote Backup Provider Metadata

Settings can store optional remote backup provider metadata. The first provider
type is Google Drive, but the storage model is provider-based so future Dropbox,
S3-compatible, or self-hosted storage providers can share the same local UI and
record shape.

Provider metadata lives inside the unlocked SQLCipher database. Provider secrets,
when present, are encrypted with the local gateway vault and are never returned
by list/detail API responses.

Google Drive uses an explicit local UI authorization flow. The user supplies a
Google OAuth client id and client secret for the provider, opens the Google
verification URL, and confirms the device code. Create that OAuth client in
Google Cloud Console as an OAuth client for TVs and limited-input devices, and
enable Google Drive API on that Google Cloud project. AIPermission stores the
client secret and resulting token payload only as encrypted provider secret; it
does not expose either value to MCP or to provider list/detail responses.

See the [Google Drive backup provider guide](../providers/google-drive.md) for
step-by-step setup.

After Google Drive is connected, `Upload backup` creates a temporary SQLCipher
snapshot of the unlocked database and uploads the encrypted `.aipdb` blob to the
configured Drive folder. The local database password is never uploaded. A local
backup record stores the provider file id, filename, size, checksum, source
machine, and timestamps.

Remote backup records can be downloaded back as encrypted `.aipdb` files or
restored as a new local database. Restore downloads the remote blob, verifies
stored size/checksum metadata, validates the user-provided database password,
and imports the backup under a new local database name. Restore never overwrites
the currently open database.

This does not make AIPermission a remote gateway. A remote backup provider stores
encrypted `.aipdb` blobs as-is. It does not receive MCP tokens, connector
credentials, SSH keys, database passwords, or the ability to decrypt a database.

Continuous two-way sync is intentionally not part of the current model. Remote
backup provider actions are explicit user-initiated storage operations, and
restore still requires the database password before a local database can be
opened.

## Security Notes

- `.aipdb` files are sensitive but should be SQLCipher encrypted.
- The database password is not stored next to the file.
- Backups created before a password change open with the old password.
- Import must fail with the wrong database password.
- Private keys must not appear in API responses after import.
- Backup requires an unlocked database.
