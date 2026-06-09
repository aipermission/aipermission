# REST API

This document tracks the public-ish REST surface used by the web UI and MCP bridge. The API is local-only and assumes the encrypted database has been unlocked unless the endpoint is part of setup/unlock.

The web REST API is not a remote multi-user API. After database unlock, protected web REST endpoints require a local HttpOnly browser session cookie. Mutating web REST requests also send a double-submit CSRF header/cookie pair. MCP endpoints do not use that cookie; they authenticate with API tokens.

## Error Shape

Errors use a JSON object:

```json
{
  "error": "message"
}
```

Request bodies reject unknown fields where structured decoding is used.

## Health And Unlock

```txt
GET  /health
GET  /api/status
GET  /api/unlock/status
POST /api/unlock/setup
POST /api/unlock
POST /api/backup/import
POST /api/databases/switch
POST /api/databases/rename
POST /api/databases/delete
POST /api/databases/change-password
POST /api/lock
```

`/health` is a lightweight process health check and does not require an unlocked database.

`/api/status` returns public gateway status and configuration shape for the web UI. It does not expose local database file paths.

Most `/api/*` application endpoints return `423 Locked` until a database is unlocked. Setup/import/unlock endpoints remain available while locked. After a database is unlocked, protected web REST endpoints return `401 Unauthorized` if the local browser session cookie is missing or invalid.

Database unlock is process state. Closing the browser does not lock the backend. If the browser session cookie is deleted or expires while the backend remains unlocked, `GET /api/unlock/status` returns `state: "session_required"` and the UI asks for the same database password to issue a new session cookie. Unlock status lists database IDs/names/states for the UI, but omits local filesystem paths. `Switch` can move the UI context to another already-unlocked database without stopping work in the previous workspace.

If a target database is locked, `switch` requires its password. If the same API token exists in more than one unlocked database, MCP authentication returns `409 Conflict`.

## Servers

```txt
GET    /api/servers
POST   /api/servers
GET    /api/servers/{id}
PUT    /api/servers/{id}
DELETE /api/servers/{id}
POST   /api/servers/{id}/test
POST   /api/servers/{id}/docker-check
POST   /api/servers/{id}/docker-logs
POST   /api/servers/test-connection
POST   /api/ssh-host-keys/approve
```

Server responses never include SSH private keys or decrypted credential payloads.

Create/update shape:

```json
{
  "name": "worker-2",
  "host": "159.69.12.186",
  "port": 22,
  "username": "root",
  "ssh_key_id": 7,
  "description": "maintenance target",
  "startup_input_after_connect": "",
  "force_shell_command": ""
}
```

`startup_input_after_connect` and `force_shell_command` are optional advanced
SSH compatibility settings. They are intended for appliances that show an
interactive menu before a normal shell, such as some NAS devices. The startup
input is written exactly to the PTY after connect. The forced shell command
starts that command instead of the default SSH shell. Leave both empty for
normal Linux servers.

Server-specific custom hints are not accepted by the server CRUD API in the current MVP. MCP `list_servers` may still return gateway-generated operational hints, such as safe package verification or bounded log commands.

`POST /api/servers/test-connection` tests an unsaved form payload before save, unless the user chooses to set up the server later.

`POST /api/servers/{id}/docker-check` runs a read-only, on-demand Docker status command over the server's SSH connection. It does not persist inventory or poll in the background. The response includes whether Docker is available and the current running containers:

```json
{
  "server_id": 3,
  "server_name": "worker-2",
  "available": true,
  "ok": true,
  "containers": [
    {
      "id": "abc123",
      "name": "web",
      "image": "nginx:alpine",
      "status": "Up 2 minutes",
      "ports": "0.0.0.0:8080->80/tcp"
    }
  ],
  "exit_code": 0,
  "duration_ms": 320
}
```

If Docker is installed but the status command fails, the response keeps `available: true` and returns `ok: false` with stderr/stdout details. The UI shows this as a Docker access/service problem rather than as an empty container list.

`POST /api/servers/{id}/docker-logs` reads the latest logs for one container from the selected server. The request body is:

```json
{
  "container_ref": "abc123",
  "tail": 300
}
```

The backend runs a bounded `docker logs --tail N --timestamps` command over SSH and returns stdout, stderr, exit code, and duration. `tail` is optional, defaults to 300, and is capped at 5000 lines. This endpoint is on-demand; it does not poll or persist Docker inventory.

If a test or SSH-backed action reaches an unknown host key, the backend returns:

```json
{
  "error": "ssh host key approval required",
  "code": "unknown_ssh_host_key",
  "host_key": {
    "host": "159.69.12.186",
    "port": 22,
    "hostname": "159.69.12.186:22",
    "key_type": "ssh-ed25519",
    "fingerprint_sha256": "SHA256:...",
    "public_key": "BASE64_HOST_PUBLIC_KEY"
  }
}
```

The UI asks the user to verify and approve the fingerprint. `POST /api/ssh-host-keys/approve` records the key in the local `known_hosts` file.

`DELETE /api/servers/{id}` deletes only the local record.

`DELETE /api/servers/{id}?remove_key=true` first connects with the server's gateway key, removes remote `~/.ssh/authorized_keys` entries containing that public key blob, then deletes the local record. This handles changed comments or authorized_keys options. If remote cleanup fails or removes zero entries, the local record is kept.

## Console Commands

```txt
POST /api/console/bulk-exec
GET  /api/console/sessions
POST /api/console/sessions
GET  /api/console/sessions/{id}
POST /api/console/sessions/{id}/input
POST /api/console/sessions/{id}/close
GET  /api/console/sessions/{id}/attach
POST /api/console/servers/{id}/restart
```

`POST /api/console/bulk-exec` runs one local-UI command across selected servers.
It is not an MCP tool. The command runs through each target server's persistent
SSH console session and creates one `source: "manual"` command history row per
server.

The request body requires an exact confirmation string based on the selected
server count:

```json
{
  "server_ids": [3, 4, 7],
  "command": "apt update",
  "reason": "weekly package metadata refresh",
  "confirmation": "RUN ON 3 SERVERS"
}
```

The backend validates duplicate IDs, command size, and confirmation text before
creating history rows. Execution is limited to a small parallelism window so a
large server selection does not fan out all SSH sessions at once. The response
returns the command request IDs; poll `GET /api/approvals/{id}` or use the
History page for per-server output, exit code, error, and status:

```json
{
  "parallelism": 3,
  "items": [
    {
      "request_id": 101,
      "server_id": 3,
      "server_name": "worker-1",
      "status": "running"
    }
  ]
}
```

## File Transfers

```txt
GET  /api/file-transfers
GET  /api/file-transfers/{id}
GET  /api/file-transfers/{id}/download
POST /api/file-transfers/{id}/cancel
GET  /api/file-transfer-batches
GET  /api/file-transfer-batches/{id}
GET  /api/file-transfer-batches/{id}/download
POST /api/file-transfer-batches/{id}/pause
POST /api/file-transfer-batches/{id}/resume
POST /api/file-transfer-batches/{id}/cancel
POST /api/file-transfer-batches/{id}/queue
POST /api/file-transfer-batches/{id}/approve
POST /api/file-transfer-batches/{id}/decline
POST /api/file-transfers/browse
POST /api/file-transfers/upload
POST /api/file-transfers/upload-batch
POST /api/file-transfers/download
POST /api/file-transfers/download-batch
GET  /api/mcp/file-transfers
GET  /api/mcp/file-transfers/{id}
GET  /api/mcp/file-transfer-batches
GET  /api/mcp/file-transfer-batches/{id}
POST /api/mcp/file-transfers/browse
POST /api/mcp/file-transfers/upload-batch
POST /api/mcp/file-transfers/download-batch
GET  /api/mcp/file-transfer-batches/{id}/download
POST /api/mcp/file-transfer-batches/{id}/pause
POST /api/mcp/file-transfer-batches/{id}/resume
POST /api/mcp/file-transfer-batches/{id}/cancel
```

File transfers use the selected server's existing SSH credential and run over
SFTP. AIPermission stores transfer metadata, status, progress, and checksum
only. File contents are never stored in SQLCipher. Uploads and downloads use
private short-lived temporary staging files under the local data directory.

The local UI can upload local files and download remote files. MCP can list
transfer status, browse remote directories, start remote download queues, save
completed downloads to explicit local paths, upload explicitly named local
files, and pause/resume/cancel queues. MCP transfer management requires
`always_run` or `approval_required` permission for that server. `always_run`
starts the queue immediately. `approval_required` creates a
`pending_approval` queue in the local Transfer Center; only locally approved
items are copied. MCP tool responses never include file contents, local
temporary paths, archive staging paths, or local upload contents.

`GET /api/file-transfers` returns paginated transfer history. Optional filters
include `direction`, `status`, `server_id`, and `q`:

```txt
GET /api/file-transfers?paginated=true&direction=download&status=completed&q=backup
```

`POST /api/file-transfers/browse` lists one remote directory through SFTP so the
local UI can select upload/download paths:

```json
{
  "server_id": 3,
  "path": "/home/deploy"
}
```

`POST /api/file-transfers/upload` accepts `multipart/form-data`:

```txt
server_id=3
remote_path=/tmp/app.log
overwrite=false
file=<browser selected file>
```

Uploads are staged in a private local temporary directory and then copied to a
temporary file beside the remote target. AIPermission moves the temporary remote
file into place only after the upload completes. Canceling or failing an upload
therefore avoids leaving a partial target file behind; the gateway also attempts
to remove the temporary remote file. The local staging file is removed after the
remote transfer finishes or fails. Uploads do not overwrite an existing regular
remote file unless `overwrite=true` is sent after an explicit local UI
confirmation. Existing directories or special files are rejected.

`POST /api/file-transfers/download` starts a remote file download:

```json
{
  "server_id": 3,
  "remote_path": "/var/log/syslog"
}
```

The backend downloads to a private temporary file and returns `202 Accepted`
with the transfer record. Poll `GET /api/file-transfers/{id}` until the status
is `completed`, then use `GET /api/file-transfers/{id}/download` to download the
temporary file through the browser. Temporary download files are short-lived and
may return `410 Gone` after cleanup.

`POST /api/file-transfers/{id}/cancel` cancels a pending or running transfer and
closes the active SFTP operation when it is still in progress.

The local UI primarily uses the batch queue endpoints. `POST
/api/file-transfers/upload-batch` accepts `multipart/form-data`:

```txt
server_id=3
remote_dir=/home/deploy
overwrite=false
files=<browser selected file>
files=<another browser selected file>
```

The backend stages each selected file in a private temporary directory, creates
a batch record and per-file transfer records, then copies files sequentially
over SFTP. If any target file already exists and `overwrite=false`, the endpoint
returns `409 Conflict` with `code: "remote_files_exist"` and a `conflicts`
array. The UI asks for explicit confirmation before retrying with
`overwrite=true`. Duplicate target paths in the same queue are rejected before
the transfer starts.

`POST /api/file-transfers/download-batch` starts a queued remote download:

```json
{
  "server_id": 3,
  "remote_paths": ["/var/log/syslog", "/var/log/auth.log"],
  "archive_name": "logs.zip"
}
```

Remote files are downloaded sequentially to private temporary files. A single
download is served as the downloaded file. Multiple completed downloads are
packaged into a temporary zip, then served through `GET
/api/file-transfer-batches/{id}/download`. Duplicate remote paths in the same
queue are rejected. If multiple files have the same basename, the generated zip
uses numeric suffixes to keep entries unique. A download batch is limited to
1 GiB total remote file size.

`GET /api/file-transfer-batches/{id}` returns the batch record, aggregate
progress, speed/ETA, and ordered per-file items. `POST
/api/file-transfer-batches/{id}/pause` pauses the active queue while the current
gateway process keeps running. `resume` continues the same in-process transfer
where practical; if the gateway process, Docker container, or computer restarts,
unfinished queues should be started again. `cancel` cancels the active queue and
cleans staged temporary files.

`POST /api/file-transfer-batches/{id}/queue` edits pending items in a paused
queue:

```json
{
  "item_ids": [12, 14, 13]
}
```

Only pending items can be reordered or removed. Omitted pending items are
removed from the queue; already running, completed, failed, or canceled items
are not modified. Removed pending items are removed from transfer history because
they were never copied.

`POST /api/file-transfer-batches/{id}/approve` approves a pending MCP transfer
queue. The body selects the item ids that may run; unchecked pending items are
rejected and kept in history as canceled items with the note:

```json
{
  "item_ids": [12, 14],
  "note": "Skip auth.log because it is not the latest file."
}
```

`POST /api/file-transfer-batches/{id}/decline` rejects the whole pending
approval queue:

```json
{
  "note": "Please send the latest archive instead."
}
```

Remote paths must be absolute file paths. Directory transfer, recursive copy,
remote glob expansion, restart-surviving resumable transfers, and
SSH-agent/ProxyJump based transfers are not part of this MVP.

MCP transfer status endpoints return token-scoped, sanitized transfer metadata.
They never include local temporary paths or archive staging paths:

```txt
GET /api/mcp/file-transfers?server_id=3&direction=download&status=running
GET /api/mcp/file-transfer-batches?server_id=3&limit=20
GET /api/mcp/file-transfer-batches/{id}
```

MCP transfer control endpoints use the MCP API token rather than the UI session
cookie:

```json
{
  "server_id": 3,
  "remote_paths": ["/var/log/syslog", "/var/log/auth.log"],
  "archive_name": "logs.zip"
}
```

The response includes `retry_after_seconds` and `assistant_hint`; the AI should
poll `GET /api/mcp/file-transfer-batches/{id}` for progress. If the status is
`pending_approval`, the AI should wait for the local operator decision. A local
MCP client can then call `GET /api/mcp/file-transfer-batches/{id}/download`
through the package `save_file_download` tool to write the completed download
to an explicit local path. MCP uploads use `POST
/api/mcp/file-transfers/upload-batch` with explicit local files supplied by the
local MCP package.

## SSH Keys

```txt
GET    /api/ssh-keys
POST   /api/ssh-keys
POST   /api/ssh-keys/import
GET    /api/ssh-keys/{id}
DELETE /api/ssh-keys/{id}
GET    /api/ssh-config/discover
POST   /api/ssh-config/parse
```

Create shape:

```json
{
  "name": "main",
  "key_type": "ed25519"
}
```

Supported `key_type` values:

```txt
ed25519
rsa
```

Import shape:

```json
{
  "name": "existing-laptop-key",
  "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----",
  "passphrase": "optional import-time passphrase"
}
```

Imported keys support common OpenSSH private key formats, including ed25519,
rsa, and ecdsa keys that the backend can parse. Imported RSA keys must be at
least 2048 bits. If the source key is
passphrase-protected, the passphrase is used only to parse the key during
import. AIPermission then stores a normalized private key inside the encrypted
local vault. The original passphrase is not stored.

Response may include public key, fingerprint, and install command. It must not include the private key.

`GET /api/ssh-config/discover` reads SSH host metadata from the gateway process user's
`~/.ssh/config` when that file is available. Docker installs may not expose the
host user's SSH config unless the user mounted it deliberately.

`POST /api/ssh-config/parse` parses explicit SSH host config content selected
from a local file or pasted by the user:

```json
{
  "content": "Host worker\n  HostName 10.0.0.42\n  User ubuntu\n"
}
```

Host import returns concrete host entries with alias, host, port, username,
identity file path, proxy jump metadata, whether a `ProxyCommand` is configured,
and warnings where OpenSSH tokens are present. The raw `ProxyCommand` value is
not returned. It does not import private key material silently. Wildcard-only
blocks such as `Host *` are not returned as servers, but matching fields are
applied in OpenSSH-style first-value-wins order. Docker installs should use
explicit file parsing or pasted config content unless the host SSH config was
deliberately mounted into the gateway container.

## Tokens

```txt
GET    /api/tokens
POST   /api/tokens
POST   /api/tokens/{id}/revoke
GET    /api/tokens/{id}/permissions
PUT    /api/tokens/{id}/permissions
GET    /api/settings/security
PUT    /api/settings/security
GET    /api/settings/retention
PUT    /api/settings/retention
POST   /api/settings/retention/purge
GET    /api/settings/redaction-rules
POST   /api/settings/redaction-rules
PUT    /api/settings/redaction-rules/{id}
DELETE /api/settings/redaction-rules/{id}
```

Token create returns the token value once. `expires_at` is optional and must be
an RFC3339 timestamp in the future when present:

```json
{
  "name": "codex-maintenance",
  "expires_at": "2026-06-01T14:00:00Z"
}
```

`GET /api/tokens` does not return token values by default. If reusable token
copy is enabled in Security, token values created after that setting is enabled
are stored encrypted and can be returned for UI copy. Revoked or expired tokens
are rejected by MCP endpoints.

Security settings:

```json
{
  "reusable_tokens": false,
  "expose_mcp_server_metadata": false,
  "redaction_mode": "basic"
}
```

`expose_mcp_server_metadata` controls whether MCP `list_servers` includes `host`, `port`, and `username`. `redaction_mode` is `basic` or `off`; basic redaction masks common token/password/API-key/private-key patterns before command history, console transcripts, and audit payloads are persisted or returned through MCP. Approval execution stores a separate encrypted raw command payload internally so the approved command still runs exactly as submitted while UI/history/audit display fields remain redacted.

When `redaction_mode` is `basic`, custom redaction rules can be added on top of the built-in patterns:

```json
{
  "name": "Internal token",
  "pattern": "(?i)internal_[a-z0-9]{24,}",
  "enabled": true
}
```

Patterns use Go RE2 syntax, are limited in size, and replace matches with `[REDACTED]`. Custom rules are stored in the encrypted database and move with `.aipdb` backups/imports.

Retention settings:

```json
{
  "history_days": 0,
  "audit_days": 0,
  "console_days": 0,
  "message_days": 0
}
```

`0` disables automatic cleanup for that category. Cleanup runs when a database is unlocked and immediately after retention settings are saved. `POST /api/settings/retention/purge` runs a one-time manual purge:

```json
{
  "target": "history",
  "days": 30
}
```

Valid targets are `history`, `audit`, `console`, and `messages`.

Permission update shape:

```json
{
  "permissions": [
    {
      "server_id": 3,
      "execution_rule": "approval_required",
      "expires_at": "2026-06-07T14:00:00Z"
    }
  ]
}
```

`expires_at` is optional and must be an RFC3339 timestamp in the future when
present. It creates a temporary token/server permission grant. Expired grants
remain visible in the local UI for clarity, but MCP permission checks no longer
treat them as effective.

Supported execution rules:

```txt
always_run
approval_required
blocked
```

`PUT` replaces the full permission set for the token. Servers not included are inaccessible to that token.

Permissions can be edited from the Console token panel or from the Tokens page dot/dialog flow.

## Backup And Import

```txt
GET  /api/backup/download
POST /api/backup/import
```

`GET /api/backup/download` returns the active SQLCipher database as a binary `.aipdb` file. The backend creates a temporary SQLCipher snapshot and serves that snapshot instead of streaming the live database file directly.

`POST /api/backup/import` should use `multipart/form-data`:

```txt
sqlite=<database file>
database_name=Project Alpha
database_password=DATABASE_PASSWORD
```

The multipart field name `file` is accepted as a compatibility alias, but the UI and current examples use `sqlite`. JSON/base64 database import is not supported; use multipart so the backend can stream the uploaded file to a temporary encrypted import path.

Import can run while locked. The backend validates the uploaded database with the provided password, stores it as a named local database, and unlocks it. Import never overwrites an existing database file; colliding names are made unique or rejected instead of replacing data.

Older `.aipbackup` JSON export/restore endpoints are no longer registered in the public REST surface. Use `.aipdb` download/import instead.

## Console Sessions

```txt
POST   /api/console/exec
GET    /api/console/sessions
POST   /api/console/sessions
GET    /api/console/sessions/{id}
POST   /api/console/sessions/{id}/input
POST   /api/console/sessions/{id}/close
GET    /api/console/sessions/{id}/attach
POST   /api/console/servers/{id}/restart
```

`POST /api/console/exec` is a direct local web/API command endpoint kept for compatibility. The current Console UI uses persistent sessions instead.

The backend owns the SSH shell. Browser and MCP clients attach to the same `session_id`; if the browser closes while Docker/backend keeps running, the shell and transcript remain in the backend. Recent transcript text is kept as a bounded session snapshot, while the persistent stream is also stored as append-only chunks for long-running sessions.

Console websockets are locally hardened with bounded message size, client count, read deadlines, ping/pong keepalive, and lightweight input/resize frequency limits. These are abuse guardrails for the local gateway; they are not a remote multi-user quota system.

`close_existing=true` closes any open shell for the same server and starts a new one. The UI New Session action uses this.

`POST /api/console/servers/{id}/restart` is the local UI recovery action for a
stuck persistent console session. It closes live console sessions for that
server, marks running command requests for that server as `error`, writes an
audit event, and lets the next command open a fresh SSH session. This route is
protected by the UI session and CSRF checks.

Attach WebSocket messages from the server include:

```json
{ "type": "snapshot", "status": "connected", "data": "...", "session_id": 12 }
{ "type": "ready", "status": "connected", "session_id": 12 }
{ "type": "output", "status": "connected", "data": "..." }
{ "type": "error", "data": "PTY error" }
{ "type": "exit", "status": "closed", "data": "" }
```

Client messages include:

```json
{ "type": "input", "data": "ls\n" }
{ "type": "resize", "cols": 120, "rows": 30 }
```

SSH connections use a gateway `known_hosts` file under the data path. The first unknown host key returns `409 unknown_ssh_host_key` with a SHA256 fingerprint. After the user approves that fingerprint, later mismatches are rejected.

## MCP HTTP Endpoints

The npm MCP bridge uses local credential-safe HTTP endpoints:

```txt
GET  /api/mcp/servers
POST /api/mcp/exec
GET  /api/mcp/requests/{id}
GET  /api/mcp/requests
GET  /api/mcp/console
POST /api/mcp/console/restart
POST /api/mcp/messages
```

MCP endpoints authenticate with the API token. They reject revoked tokens and check token/server permissions.

`GET /api/mcp/servers` returns only servers visible to that token and not blocked.

The web UI also exposes `GET/PUT /api/settings/mcp-runtime` for the local user.
That route is protected by the UI session and CSRF checks, not by MCP token auth.
It controls whether new MCP command execution is currently Started or Stopped.
Saved token/server permissions are preserved while stopped.

`POST /api/mcp/exec` applies the execution rule:

- `always_run`: run in the persistent console session
- `approval_required`: create pending approval and return `approval_pending`
- `blocked`: reject without execution
- global stopped runtime: return `stopped` without execution

For approval-required commands, the bridge should poll `get_request` according to `assistant_hint`. If the user clicks Run, the backend executes in the persistent console session. If the user clicks Decline, the request becomes `declined`.

For long `always_run` commands, `/api/mcp/exec` may return `running` with `retry_after_seconds` and `assistant_hint`. The AI should poll `get_request(request_id)` and use `read_console(server_id)` for live output before sending another long-running command to the same server.

If a persistent console session appears stuck, `POST /api/mcp/console/restart`
closes the current console session for that server, marks any running command
requests for that server as `error`, and lets the next `/api/mcp/exec` open a
fresh SSH session. The route requires a non-blocked token/server permission and
the global MCP runtime must be Started.

## Approvals

```txt
GET  /api/approvals
GET  /api/approvals/{id}
POST /api/approvals/{id}/run
POST /api/approvals/{id}/decline
POST /api/approvals/{id}/labels
DELETE /api/approvals/{id}/labels/{label_id}
GET  /api/history-labels
POST /api/history-labels
DELETE /api/history-labels/{id}
```

`GET /api/approvals` returns recent command requests. Optional filters include `status`, `source`, `server_id`, and `label_id`. `source` is `mcp` for MCP/approval command requests and `manual` for manually typed Console commands.

The History page uses paginated search. `q` searches command text, reason, status, captured output, error, server name, and token name. Command text and output fields use SQLCipher-backed FTS4 indexes; server and token names remain regular filtered fields:

```txt
GET /api/approvals?paginated=true&limit=50&offset=0&q=docker&source=mcp&status=completed&server_id=3&label_id=4
```

History response items include `source`, `tracking_reason`, and `output_truncated`. Manual Console command logging records typed or pasted terminal input as `source = manual`. For simple commands, AIPermission uses the normal PTY transcript to capture output when the shell prompt returns, then marks the row `completed` or `canceled`. Because the gateway does not install shell hooks or append hidden command suffixes, it cannot reliably infer every interactive shell state. Interactive commands, nested shells, heredocs, and unsafe control sequences are stored as `untracked` best-effort rows, with output still available in the Console transcript. Arrow/history recall uses a placeholder command because the terminal does not send the recalled command text; simple recalled commands may still capture output when the prompt returns, while ambiguous interactive recalled commands are left `untracked`.

The paginated response is an envelope:

```json
{
  "items": [],
  "total": 120,
  "limit": 50,
  "offset": 0,
  "next_offset": 50
}
```

Paginated list responses omit full stdout/stderr. `GET /api/approvals/{id}` returns the detail payload with captured output.

History labels are user-managed tags for command requests. They do not change command execution or audit behavior. Use `POST /api/history-labels` to create or return an existing label. New labels return `201 Created`; reused labels return `200 OK`:

```json
{
  "name": "issue-440",
  "color": "#0f766e"
}
```

Use `POST /api/approvals/{id}/labels` to attach an existing label by `label_id`, or create-and-attach by `name`:

```json
{
  "name": "docker"
}
```

Deleting a label removes its command-request relationships. The command history records remain intact, and filtering by the deleted label returns no entries.

Run changes a `pending_approval` request to `running` and starts execution in the backend-owned console session. It accepts an optional JSON body with `user_note`; when provided, the note is delivered to the matching MCP token through the message queue. The request later becomes `completed`, `failed`, or `error`.

Approval-required MCP commands store an approval-context snapshot when the
pending request is created. If the token, token/server permission, server
profile, SSH key fingerprint, MCP tool metadata, or command payload hash changes
before Run, the backend marks the request `stale` and returns `409 Conflict`.
The AI should submit a fresh `exec` request.

Decline changes the request to `declined`. The optional `user_note` is stored on the command request and returned to MCP as operator guidance.

## Messages

```txt
GET  /api/messages
POST /api/messages
POST /api/messages/read
POST /api/mcp/messages
```

`POST /api/messages` creates a user-to-AI note:

```json
{
  "token_id": 2,
  "server_id": 3,
  "session_id": 12,
  "message": "Also inspect Docker logs on the next check."
}
```

User-to-AI messages are token-scoped. If `server_id` is set, the note is consumed only by matching server responses. If `session_id` is also set, it is consumed only by MCP responses attached to that exact persistent console session. Generic notes can omit both `server_id` and `session_id`.

`POST /api/mcp/messages` is authenticated with the MCP token and writes an AI-to-user message. If the message is attached to a server, the token must have permission for that server.

Unread AI-to-user messages contribute to Console sidebar and server list badge counts. Opening the Messages drawer can mark matching messages as read.

## Audit

```txt
GET /api/audit-logs
GET /api/audit-logs/{id}
```

Returns paginated audit events. Optional filters include `q`, `actor`, and `server_id`. Audit action and payload search use SQLCipher-backed FTS4 indexes; server and token names remain regular filtered fields:

```txt
GET /api/audit-logs?limit=50&offset=0&q=docker&actor=mcp&server_id=3
```

List responses use the same pagination envelope as History and include a payload preview. `GET /api/audit-logs/{id}` returns the full payload.

Token create/revoke, permission changes, security settings changes, retention cleanup, console lifecycle/input, MCP execution states, and approval decisions are written.

Secret payloads, SSH private keys, and token values must not be written to audit logs.

Command text is stored in audit/history records. Users should avoid putting secret values directly in command strings and should be cautious when printing files or environment output that may contain secrets.
