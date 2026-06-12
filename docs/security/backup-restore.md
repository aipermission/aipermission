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

## Security Notes

- `.aipdb` files are sensitive but should be SQLCipher encrypted.
- The database password is not stored next to the file.
- Backups created before a password change open with the old password.
- Import must fail with the wrong database password.
- Private keys must not appear in API responses after import.
- Backup requires an unlocked database.
