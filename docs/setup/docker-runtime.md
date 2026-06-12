# Docker Runtime

The default runtime model for the MVP is local Docker.

## Compose Shape

```txt
docker compose up
```

On Windows, keep shell scripts checked out with LF line endings. Git should do
this automatically through the repository `.gitattributes` file; if
`docker compose up -d --build` fails with `exec /entrypoint.sh: no such file or
directory`, reset the checkout line endings and rebuild the images.

Expected local endpoints:

```txt
frontend        -> http://localhost:3210
web API proxy   -> http://localhost:3210/api
MCP URL         -> http://localhost:3210
```

Compose publishes host ports only on `127.0.0.1` by default. This is an intentional security boundary. After unlock, the web REST API uses a local HttpOnly browser session cookie, but that cookie is a localhost UX/session guard and not a remote multi-user auth model.

The backend refuses to start when `AIPERMISSION_BACKEND_HOST` is `0.0.0.0` or any non-loopback address. In Docker Compose, the backend shares the frontend network namespace, binds only to `127.0.0.1`, and the frontend nginx proxies `/api` to it. Do not change Compose port bindings to `0.0.0.0` or a LAN address. The localhost bind is the security boundary; Host-header checks are defense in depth only. Remote/LAN access is unsupported.

This is not meant to be bypassed with a reverse proxy or a Compose override. The gateway is not designed for remote hosting, LAN sharing, or team access. Remote machines belong in the Connectors list as SSH targets; they should not host the AIPermission web/API gateway for other clients.

If the default frontend port is occupied, it can be changed through environment variables.

## MCP URL

MCP clients should use the local gateway URL:

```txt
http://localhost:3210
```

For MCP setup, see [MCP Client Setup](mcp-client-setup.md).

## Data Persistence

SQLite data should live in a Docker volume or explicit data path.

The target data shape:

```txt
data/
  databases/
  gateway.secret
  known_hosts
```

The gateway vault secret is stored under the same data path as `gateway.secret`. It is also copied into unlocked encrypted DB settings as `settings.gateway_secret`, so a downloaded `.aipdb` can restore SSH key payloads on another machine.

SSH host key records live in `known_hosts`. The first unknown host key must be approved from its SHA256 fingerprint; after approval, later key changes are rejected. This file is shared by every named database in the same local data directory, so host key trust is scoped to the local gateway data directory rather than to one database file.

SQLite databases are encrypted with SQLCipher. On first web use, the user creates a database password with at least 14 characters, uppercase letters, lowercase letters, and numbers. After container restarts, the UI shows the unlock screen and requires the same password. If the browser session cookie is missing while the backend process is still unlocked, the UI asks for the same password again and issues a new local session cookie without using the database password as a request token.

Schema migrations use a versioned `schema_migrations` table. New migrations are recorded one by one. Runtime maintenance closes stale running sessions after restart, but does not delete old data. Configurable retention cleanup is stored in database settings, runs after unlock, and can also be triggered manually from Settings.

Settings Change Password verifies the current password and re-encrypts the active database. Older `.aipdb` backups still open with their original password; later backups use the new password.

Closing the browser tab does not lock the database. Unlock is backend process state so MCP/AI work can continue while the browser is closed.

Multiple named databases can remain unlocked in the same backend process. UI Switch reuses an already-unlocked target database without asking for the password and does not stop MCP work or persistent console sessions in other workspaces. If the target database is not unlocked, the switch dialog asks for that database password.

If the same API token value exists in more than one unlocked database, MCP authentication returns a conflict. The gateway does not guess the workspace; the user must revoke the duplicate token copy or lock the other database.

Lock closes the current database when only one database is unlocked. If multiple databases are unlocked, the UI asks whether to lock the current database or all databases.

If the database password is forgotten, it cannot be recovered. The user loses local DB state and can remove old public key lines from remote servers manually if needed.

If no database exists, the first screen shows Create Database and Import Database. If databases exist, the login screen asks for a database. Encrypted databases support Unlock Database, New Database, and Import Database.

New Database creates a separate named encrypted DB. Import Database imports a user-selected SQLCipher-encrypted `.aipdb` or `.db` file as a named local DB. Plain SQLite runtime files and imports are rejected.

## Environment

Expected MVP environment shape:

```env
AIPERMISSION_BACKEND_PORT=8080
AIPERMISSION_FRONTEND_PORT=3210
```

`AIPERMISSION_GATEWAY_SECRET` is optional and should be left unset for normal
local installs. On first start, the gateway generates a high-entropy local vault
secret and stores it in the Docker data volume. If explicitly set for advanced
local testing, use at least 32 random characters. This secret enters the backup
payload but is not the database password.
