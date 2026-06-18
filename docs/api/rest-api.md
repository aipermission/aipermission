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
POST /api/databases/delete-locked
POST /api/databases/change-password
POST /api/lock
```

`/health` is a lightweight process health check and does not require an unlocked database.

`/api/status` returns public gateway status and configuration shape for the web UI. It does not expose local database file paths.

Most `/api/*` application endpoints return `423 Locked` until a database is unlocked. Setup/import/unlock endpoints remain available while locked. After a database is unlocked, protected web REST endpoints return `401 Unauthorized` if the local browser session cookie is missing or invalid.

Database unlock is process state. Closing the browser does not lock the backend. If the browser session cookie is deleted or expires while the backend remains unlocked, `GET /api/unlock/status` returns `state: "session_required"` and the UI asks for the same database password to issue a new session cookie. Unlock status lists database IDs/names/states for the UI, but omits local filesystem paths. `Switch` can move the UI context to another already-unlocked database without stopping work in the previous workspace.

If a target database is locked, `switch` requires its password. If the same API token exists in more than one unlocked database, MCP authentication returns `409 Conflict`.

`POST /api/databases/delete-locked` is available from the locked unlock screen
for local database cleanup. It requires the selected database id and that
database password, validates the encrypted file without running schema
migrations, and deletes only the local database file. It is intended for
removing old pre-0.2 source databases after migration.

## Connector Catalog And Targets

```txt
GET /api/connectors
GET /api/connectors/{kind}
GET /api/targets
GET /api/connector-targets
GET /api/connector-targets/inventory
POST /api/connector-targets/with-profile
POST /api/connector-targets
POST /api/connector-targets/ping
POST /api/connector-targets/test
GET /api/connector-targets/{id}
PUT /api/connector-targets/{id}/with-profile/{profile_id}
PUT /api/connector-targets/{id}
DELETE /api/connector-targets/{id}
POST /api/connector-targets/{id}/operations/{operation}
GET /api/connector-targets/{id}/profiles
POST /api/connector-targets/{id}/profiles
POST /api/connector-targets/{id}/profiles/{profile_id}/provision
PUT /api/connector-targets/{id}/profiles/{profile_id}
DELETE /api/connector-targets/{id}/profiles/{profile_id}
POST /api/connector-targets/{id}/profiles/{profile_id}/test
GET /api/connector-targets/{id}/profiles/{profile_id}/actions
POST   /api/ssh-host-keys/approve
```

Connector catalog endpoints expose built-in connector metadata for the local
UI. They do not return credential secrets and do not execute actions.
Connector-owned credential resource endpoints, such as SSH key resources, are
listed in [Connector Credential Resources](#connector-credential-resources).

`GET /api/connectors` returns stable connector summaries:

```json
{
  "items": [
    {
      "kind": "postgres",
      "label": "Postgres",
      "version": "0.2"
    },
    {
      "kind": "ssh",
      "label": "SSH",
      "version": "0.2"
    }
  ]
}
```

`GET /api/connectors/{kind}` returns connector metadata, target schema,
credential schemas, and generic AI-readable help text. The connector action
catalog is stable for a connector kind; the target/profile actions route below
returns that same action shape with the selected target/profile context.

`GET /api/targets` returns the unified target/profile list used by the console
and permission UI. It includes SSH targets represented as connector refs such as
`ssh:3:5` plus structured connector profiles such as `postgres:7:11`. Secret
payloads are never included.

`GET /api/connector-targets/inventory` returns active targets, active profiles,
and connector action definitions in one response for permission-management UI.
Use it when a screen needs the target/profile/action graph; use
`GET /api/connector-targets` for lightweight target lists.

Connector target responses never include decrypted credential payloads. A target
contains non-secret connector configuration, and one or more credential profiles
contain public credential metadata plus encrypted secret material that is only
resolved during approved execution.

Generic target create shape:

```json
{
  "connector_kind": "postgres",
  "name": "main-db",
  "config": {
    "connection_mode": "direct",
    "host": "127.0.0.1",
    "port": 5432,
    "database": "app",
    "ssl_mode": "require",
    "transport_target_ref": ""
  }
}
```

Postgres supports `connection_mode=direct` and `connection_mode=over_ssh`.
When `over_ssh` is used, `host` and `port` are resolved from the selected SSH
server and `transport_target_ref` must point at an SSH connector profile such as
`ssh:3:5`. Postgres defaults to `ssl_mode=require`. `prefer` and `disable` are
available for local lab databases or trusted SSH-tunneled databases, but they
weaken transport security and should be a deliberate operator choice.

`POST /api/connector-targets/with-profile` creates a connector target and its
initial credential profile in one database transaction:

```json
{
  "target": {
    "connector_kind": "postgres",
    "name": "main-db",
    "config": { "host": "127.0.0.1", "port": 5432, "database": "app" }
  },
  "profile": {
    "kind": "username_password",
    "label": "readonly",
    "public": { "username": "app_reader" },
    "secret": { "password": "..." }
  }
}
```

SSH target create/update shape:

```json
{
  "connector_kind": "ssh",
  "name": "worker-2",
  "config": {
    "host": "159.69.12.186",
    "port": 22,
    "description": "maintenance target",
    "startup_input_after_connect": "",
    "force_shell_command": ""
  }
}
```

SSH username/key material belongs to the selected credential profile, not the
target config. The UI may accept `username` and `ssh_key_id` while creating an
SSH target so it can create the first profile in one step, but saved target
config remains non-secret endpoint metadata.

`startup_input_after_connect` and `force_shell_command` are optional advanced
SSH compatibility settings. They are intended for appliances that show an
interactive menu before a normal shell, such as some NAS devices. The startup
input is written exactly to the PTY after connect. The forced shell command
starts that command instead of the default SSH shell. Leave both empty for
normal Linux servers.

Target-specific custom hints are not accepted by the connector target API in the
current MVP. Connector help may still return gateway-generated operational
hints, such as safe package verification or bounded log commands.

`POST /api/connector-targets/test` tests an unsaved connector target payload
before save when the connector supports draft tests. In 0.2.x this draft-test
route is implemented for the SSH adapter because host-key approval and
gateway-managed key selection happen before the target is saved. Other
connectors should normally use saved profile tests through
`POST /api/connector-targets/{id}/profiles/{profile_id}/test`.

`POST /api/connector-targets/ping` runs four bounded TCP reachability checks
from the selected connection mode. This is a local UI helper for connector forms
and does not use credential secrets or connector action permissions. It checks
the service port, not ICMP ping:

```json
{
  "host": "127.0.0.1",
  "port": 5432,
  "mode": "over_ssh",
  "transport_target_ref": "ssh:3:5",
  "attempts": 4
}
```

When `mode=over_ssh`, the TCP checks are dialed through the referenced SSH
connector profile so the result matches what Redis, Postgres, RabbitMQ, or a
future TCP-backed connector would see from that SSH target.

`POST /api/connector-targets/{id}/operations/docker-check` runs a read-only,
on-demand Docker status command through the SSH connector adapter. It does not
persist inventory or poll in the background. The response includes whether
Docker is available and the current running containers:

```json
{
  "target_id": 3,
  "target_name": "worker-2",
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

`POST /api/connector-targets/{id}/operations/docker-logs` reads the latest logs
for one container from the selected SSH target. The request body is:

```json
{
  "container_ref": "abc123",
  "tail": 300
}
```

The backend runs a bounded `docker logs --tail N --timestamps` command through
the SSH connector and returns stdout, stderr, exit code, and duration. `tail` is
optional, defaults to 300, and is capped at 5000 lines. This endpoint is
on-demand; it does not poll or persist Docker inventory.

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

`DELETE /api/connector-targets/{id}` removes the connector from active local
use by archiving the target and hiding its credential profiles from future
permission checks. Connector action requests, history rows, and audit records
remain readable for review. SSH targets also support `?remove_key=true`, which
first connects with the target's gateway key, removes remote
`~/.ssh/authorized_keys` entries containing that public key blob, then archives
the local target. This handles changed comments or authorized_keys options. If
remote cleanup fails or removes zero entries, the local target remains active.

`POST /api/connector-targets/{id}/profiles` creates one credential profile:

```json
{
  "kind": "username_password",
  "label": "readonly",
  "public": {
    "username": "app_readonly"
  },
  "secret": {
    "password": "local-password"
  },
  "risk_label": "read-only"
}
```

Responses include profile refs such as `postgres:7:11`, which bind a connector
kind, target id, and credential profile id without exposing the encrypted
secret payload.

`POST /api/connector-targets/{id}/profiles/{profile_id}/provision` asks a
connector to create a managed external credential through the selected profile
and then stores the generated credential profile encrypted in AIPermission. The
built-in Postgres connector uses this to create scoped database roles with
random passwords. Managed profiles carry public metadata so deleting the local
profile can run connector cleanup before the profile is archived.

`GET /api/connector-targets/{id}/profiles/{profile_id}/backup` asks a connector
with backup support to produce a downloadable backup artifact through the
selected credential profile. The built-in Postgres connector returns a plain SQL
dump generated by `pg_dump`.

`POST /api/connector-targets/{id}/profiles/{profile_id}/restore` restores a
connector backup artifact through the selected credential profile. The route
expects `multipart/form-data` with a `dump` file field and a `confirm_target`
field matching the connector target name exactly. The built-in Postgres
connector streams the uploaded SQL to `psql` with `ON_ERROR_STOP` and a single
transaction.

`PUT /api/connector-targets/{id}` updates connector target metadata and
non-secret config. Connector-specific runtime behavior, such as SSH remote-key
cleanup, host-key approval, persistent console, and SFTP-backed file transfer,
is owned by the connector implementation.

`PUT /api/connector-targets/{id}/with-profile/{profile_id}` updates the target
and one credential profile in one database transaction. Prefer it for add/edit
forms that present target and profile fields together.

`POST /api/connector-targets/{id}/operations/{operation}` runs a connector
template UI operation for one saved target when that connector adapter supports
the operation. The built-in SSH adapter currently backs on-demand Docker checks
and bounded Docker log reads through operations such as `docker-check` and
`docker-logs`. These operations are local UI helpers, not generic connector
actions and not MCP tools.

`PUT /api/connector-targets/{id}/profiles/{profile_id}` updates one credential
profile. If the `secret` object is omitted, the existing encrypted secret is
kept. If `secret` is present, the vault payload is replaced.

`DELETE /api/connector-targets/{id}/profiles/{profile_id}` archives one
connector credential profile. Archived profiles are hidden from future
permission checks, while previous action requests and history keep their target
and profile labels. Built-in SSH follows the same profile lifecycle as other
connectors: deleting a profile cancels that profile's live-console runtime, and
deleting the target handles persistent console, file-transfer, and
authorized_keys cleanup state together.
Managed connector profiles can also perform external cleanup before the local
profile is archived. For example, deleting a Postgres credential profile created
by AIPermission drops the managed database role through the admin profile that
created it.

`POST /api/connector-targets/{id}/profiles/{profile_id}/test` runs the
connector's side-effect-free connection test when the connector implements one.
The response includes `ok`, connector-specific `status`, message, and duration.

`GET /api/connector-targets/{id}/profiles/{profile_id}/actions` returns the
stable action contract for the selected target/profile pair. The route is
contextual for labels, refs, and future validation metadata; it should not make
the action catalog depend on network state or raw credential values.

Postgres `query_readonly` is defense-in-depth, not a SQL sandbox. It rejects
obvious writes plus session/transaction and dynamic-execution statements such as
`SELECT INTO`, `SET`, `NOTIFY`, `PREPARE`, and `EXECUTE`. It also runs inside a
read-only transaction, caps rows and output bytes, and applies a statement
timeout, but operators should still use dedicated
least-privilege database roles and prefer `approval_required` for ad-hoc
queries over sensitive data.

## Console Commands

```txt
POST /api/console/bulk-exec
GET  /api/console/sessions
POST /api/console/sessions
GET  /api/console/sessions/{id}
POST /api/console/sessions/{id}/input
POST /api/console/sessions/{id}/close
GET  /api/console/sessions/{id}/attach
POST /api/console/runtime-surfaces/{id}/restart
POST /api/console/targets/{id}/restart
```

`POST /api/console/bulk-exec` runs one local-UI command across selected SSH
connector targets. It is not an MCP tool. The command runs through each
target's persistent SSH console session and creates one `source: "manual"`
command history row per target.

The request body currently uses the SSH runtime ids behind the selected
connector targets and requires an exact confirmation string based on the target
count:

```json
{
  "target_ids": [3, 4, 7],
  "command": "apt update",
  "reason": "weekly package metadata refresh",
  "confirmation": "RUN ON 3 TARGETS"
}
```

The backend validates duplicate IDs, command size, and confirmation text before
creating history rows. Execution is limited to a small parallelism window so a
large target selection does not fan out all SSH sessions at once. The response
returns the command request IDs; poll `GET /api/console/command-requests/{id}`
or use the History page for per-target output, exit code, error, and status:

```json
{
  "parallelism": 3,
  "items": [
    {
      "request_id": 101,
      "target_id": 3,
      "target_name": "worker-1",
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
```

File transfers are SSH connector operations and run over SFTP through the
selected target/profile credential. AIPermission stores transfer metadata,
status, progress, and checksum only. File contents are never stored in
SQLCipher. Uploads and downloads use private short-lived temporary staging
files under the local data directory.

These endpoints are local UI endpoints for the SSH connector's SFTP transfer
capability. They use the shared target/profile identity and are normalized into
history/audit, but they are not generic connector-action REST endpoints. MCP
file-transfer support is exposed through connector actions and still goes
through token target/profile/action permission checks.

The local UI can upload local files, download remote files, and pause/resume or
cancel transfer queues. MCP uses the generic connector-action tools instead of
separate file-transfer HTTP endpoints. In 0.2.x the SSH connector exposes remote
browsing and remote-to-local download queue creation through
`browse_remote_files` and `start_file_download`; uploads, save-file dialogs, and
queue pause/resume/cancel remain local web UI operations. MCP transfer-related
responses never include file contents, local temporary paths, archive staging
paths, or local upload contents.

`GET /api/file-transfers` returns paginated transfer history. Optional filters
include `direction`, `status`, the SSH runtime `runtime_id`, and `q`:

```txt
GET /api/file-transfers?paginated=true&direction=download&status=completed&q=backup
```

`POST /api/file-transfers/browse` lists one remote directory through SFTP so the
local UI can select upload/download paths:

```json
{
  "runtime_id": 3,
  "path": "/home/deploy"
}
```

`POST /api/file-transfers/upload` accepts `multipart/form-data`:

```txt
runtime_id=3
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
  "runtime_id": 3,
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
runtime_id=3
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
  "runtime_id": 3,
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

MCP uses connector actions instead of separate file-transfer HTTP endpoints.
For SSH remote browsing and download queues, call `call_connector_action` with
the SSH connector actions `browse_remote_files` or `start_file_download`.
Transfer queue state is visible in the local Transfer Center UI.

```json
{
  "target_ref": "ssh:3:1",
  "action_name": "start_file_download",
  "input": {
    "remote_paths": ["/var/log/syslog", "/var/log/auth.log"],
    "archive_name": "logs.zip"
  },
  "reason": "Download bounded log files for local inspection."
}
```

Connector responses never include file contents, local temporary paths, or
archive staging paths.

## Connector Credential Resources

```txt
GET    /api/connectors/{kind}/credentials
POST   /api/connectors/{kind}/credentials
POST   /api/connectors/{kind}/credentials/import
GET    /api/connectors/{kind}/credentials/{id}
PUT    /api/connectors/{kind}/credentials/{id}
DELETE /api/connectors/{kind}/credentials/{id}
GET    /api/ssh-config/discover
POST   /api/ssh-config/parse
```

These endpoints manage connector-owned credential resources. In 0.2.x the
built-in SSH connector uses them for gateway-owned SSH key material. Other
connector credential profiles are managed under
`/api/connector-targets/{id}/profiles`; future connectors may add resource
handlers only when their template needs connector-owned material outside a
single target profile.

Credential resources are namespaced by connector kind and resource kind in the
frontend state, for example `ssh:ssh_key:17`. Connector-owned resource APIs
must not return raw secret material; SSH key resources expose public key
metadata and keep private key material encrypted in the local vault.

SSH credential resource create shape:

```json
{
  "name": "main",
  "key_type": "ed25519"
}
```

Supported SSH `key_type` values:

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

Update shape:

```json
{
  "name": "main-renamed"
}
```

Updates only rename the credential and refresh the public install-command
comment. They do not rotate private key material; import or generate a new
credential to rotate keys.

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
GET    /api/tokens/{id}/connector-permissions
PUT    /api/tokens/{id}/connector-permissions
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

`expose_mcp_server_metadata` controls whether MCP connector target discovery includes SSH `host`, `port`, and `username`. `redaction_mode` is `basic` or `off`; basic redaction masks common token/password/API-key/private-key patterns before command history, connector action history, console transcripts, and audit payloads are persisted or returned through MCP.

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

Connector permission update shape:

Supported execution rules:

```txt
always_run
approval_required
blocked
```

The UI may show `Disabled` when no connector permission row exists for a
token/target/profile/action. The API-level `blocked` rule is an explicit stored
deny state; omitted permissions and `blocked` both prevent execution.

```json
{
  "permissions": [
    {
      "target_id": 7,
      "profile_id": 11,
      "action_name": "query_readonly",
      "execution_rule": "approval_required",
      "expires_at": "2026-06-10T12:00:00Z"
    }
  ]
}
```

`PUT /api/tokens/{id}/connector-permissions` replaces the full connector action
permission set for the token. Each grant binds one connector target, one
credential profile, and one connector action. The response includes safe
metadata such as target name, profile label, connector kind, and target ref; it
never includes credential secrets.

`expires_at` is optional and must be an RFC3339 timestamp in the future when
present. It creates a temporary token action permission grant. Expired grants
are kept in the local database for audit clarity, but MCP discovery and
permission checks no longer treat them as effective.

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

The multipart field name must be `sqlite`. JSON/base64 database import is not supported; use multipart so the backend can stream the uploaded file to a temporary encrypted import path.

Import can run while locked. The backend validates the uploaded database with the provided password, stores it as a named local database, and unlocks it. Import never overwrites an existing database file; colliding names are made unique or rejected instead of replacing data.

Older `.aipbackup` JSON export/restore endpoints are no longer registered in the public REST surface. Use `.aipdb` download/import instead.

## Console Sessions

```txt
GET    /api/console/sessions
POST   /api/console/sessions
GET    /api/console/sessions/{id}
POST   /api/console/sessions/{id}/input
POST   /api/console/sessions/{id}/close
GET    /api/console/sessions/{id}/attach
POST   /api/console/runtime-surfaces/{id}/restart
POST   /api/console/targets/{id}/restart
```

The backend owns the SSH shell. Browser and MCP clients attach to the same `session_id`; if the browser closes while Docker/backend keeps running, the shell and transcript remain in the backend. Recent transcript text is kept as a bounded session snapshot, while the persistent stream is also stored as append-only chunks for long-running sessions.

Console websockets are locally hardened with bounded message size, client count, read deadlines, ping/pong keepalive, and lightweight input/resize frequency limits. These are abuse guardrails for the local gateway; they are not a remote multi-user quota system.

`close_existing=true` closes any open shell for the same server and starts a new one. The UI New Session action uses this.

`POST /api/console/runtime-surfaces/{id}/restart` is the local UI recovery
action for a stuck persistent console session. The id is the connector-profile
runtime surface id, not a generic connector target id. It closes live console
sessions for that runtime surface, marks running command requests for that
runtime as `error`, writes an audit event, and lets the next command open a
fresh connector runtime session. This route is protected by the UI session and
CSRF checks.

`POST /api/console/targets/{id}/restart` is a compatibility alias for the same
runtime-surface recovery action.

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
GET  /api/mcp/connector-targets
GET  /api/mcp/connector-help
GET  /api/mcp/connector-actions
POST /api/mcp/connector-actions/call
GET  /api/mcp/connector-action-requests/{id}
```

MCP endpoints authenticate with the API token. They reject revoked or expired
tokens and check token/target/profile/action permissions.

Connector MCP endpoints expose connector targets visible to the token,
AI-readable connector help, action lists, action execution, and action request
polling. `POST /api/mcp/connector-actions/call` evaluates the exact
token/target/profile/action permission before execution. `always_run` actions
execute immediately, `approval_required` actions create a pending connector
action request, and `blocked` or missing permissions do not execute.

`GET /api/mcp/connector-targets` is permission-scoped and does not perform live
health checks. By default it returns only target/profile/action identifiers,
labels, rules, expiry, and connector hints. If the local user enables
`expose_mcp_server_metadata` in Security settings, SSH target refs may also
include an allowlisted `metadata` object with `host`, `port`, and `username` so
AI clients can write clearer operator reasons. The metadata object never
contains private keys, reusable API tokens, encrypted secrets, SSH key ids, or
raw credential payloads.

The web UI also exposes `GET/PUT /api/settings/mcp-runtime` for the local user.
That route is protected by the UI session and CSRF checks, not by MCP token auth.
It controls whether new MCP connector action execution is currently Started or
Stopped. Saved token connector permissions are preserved while stopped.

## History and connector approvals

```txt
GET  /api/history
GET  /api/history/targets
GET  /api/history/{id}
POST /api/history/{id}/labels
DELETE /api/history/{id}/labels/{label_id}
GET  /api/console/command-requests/{id}
GET  /api/connector-action-approvals
GET  /api/connector-action-approvals/{id}
POST /api/connector-action-approvals/{id}/run
POST /api/connector-action-approvals/{id}/decline
POST /api/connector-actions/local-run
GET  /api/history-labels
POST /api/history-labels
DELETE /api/history-labels/{id}
```

`GET /api/history` is the canonical activity stream for connector targets.
MCP connector actions pass through the connector action token permission,
approval, history, and audit pipeline. SSH UI console/manual input and
SFTP-backed file transfers are SSH runtime adapter surfaces; they are
normalized into unified history/audit, but local UI activity is not an MCP
token approval request. `runtime_id` on these endpoints is the SSH
connector-profile runtime id kept for the live-console/file-transfer API
payloads, not a generic connector target id. Use `connector_kind` to filter by
connector type (`ssh`, `postgres`, and future connectors). `activity_type` is a
technical shape filter for API clients that need command/action/file-transfer
payloads. Use `target_id` to filter one connector target, and `profile_id` when
the target has multiple credential profiles such as `admin` and `readonly`:

```txt
GET /api/history?limit=50&offset=0&connector_kind=ssh&activity_type=file_transfer&status=completed&q=backup
GET /api/history?connector_kind=postgres&target_id=7&profile_id=11
```

`POST /api/connector-actions/local-run` is the unlocked web UI path for local
operator connector consoles. It runs one connector action as source `manual`
without MCP token permission checks, stores the action request in the shared
connector history/audit pipeline, and is protected by the UI session plus CSRF.
MCP clients must use `POST /api/mcp/connector-actions/call` instead.

`GET /api/history/targets` returns target/profile facets derived from
`history_entries`, not only currently active connector targets. Use it for
history filters so archived targets remain discoverable in past activity. Some
live-console history rows may return a runtime-only facet with
`runtime_id` and no active `target_id`/`profile_id`; pass that
`runtime_id` back to `GET /api/history` to filter those rows.

`GET /api/history/{id}` returns the detail payload for one normalized history
entry. List responses omit large output bodies; detail responses include
`input_text`, `input_json`, `output_text`, and `output_json` when available.

`GET /api/console/command-requests/{id}` returns one live-console command
request detail row for UI bulk command polling and output inspection. It is not
an approval API. MCP approvals use connector action requests only.

The History page uses paginated search. `q` searches command text, structured
connector input/output JSON, reason, status, captured output, error,
target/profile name, action name, and token name. Command text and output
fields use SQLCipher-backed FTS4 indexes where available; connector JSON,
target/profile names, action names, and token names remain regular filtered
fields:

```txt
GET /api/history?limit=50&offset=0&q=docker&connector_kind=ssh&status=completed&label_id=4
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

Paginated list responses omit full stdout/stderr. `GET /api/history/{id}` returns the normalized detail payload. `GET /api/console/command-requests/{id}` returns the raw command request detail for Console-specific flows.

History labels are user-managed tags for normalized history entries. They do not change execution, connector behavior, or audit behavior. Use `POST /api/history-labels` to create or return an existing label. New labels return `201 Created`; reused labels return `200 OK`:

```json
{
  "name": "issue-440",
  "color": "#0f766e"
}
```

Use `POST /api/history/{id}/labels` to attach an existing label to any normalized history entry by `label_id`, or create-and-attach by `name`:

```json
{
  "name": "docker"
}
```

Deleting a label removes its history-entry relationships. The history records remain intact, and filtering by the deleted label returns no entries.

Approval-required connector actions use connector action request endpoints.
`GET /api/connector-action-approvals?status=approval_pending` lists pending
connector requests for the local UI.

Run changes an `approval_pending` connector action to `running`, validates the
current token/target/profile/action permission and approval-context hash, then
decrypts the stored payload and executes the connector. It accepts an optional
JSON body with `user_note`; when provided, the note is delivered to the matching
MCP token through the message queue. The request later becomes `completed`,
`failed`, `error`, or `stale`.

If the token, connector action permission, target/profile context, credential
profile revision, connector action definition, MCP tool metadata, or prepared
payload hash changes before Run, the backend marks the request `stale`, records
a coarse `approval_context_drift` reason such as `token`, `permission`,
`target`, `profile`, `action_definition`, or `payload`, and returns `409
Conflict`. The AI should submit a fresh `call_connector_action` request.

Decline changes the connector action request to `declined`. The optional
`user_note` is stored on the connector action request and returned to MCP as
operator guidance.

Connector approval context includes token validity, permission rule,
target/profile public
metadata, profile revision, encrypted secret revision, connector kind/version,
action definition, and payload hash. If that context changed, the request
becomes `stale` and the AI must send a fresh connector call.

## Messages

```txt
GET  /api/messages
POST /api/messages
POST /api/messages/read
```

`POST /api/messages` creates a user-to-AI note:

```json
{
  "token_id": 2,
  "runtime_id": 3,
  "session_id": 12,
  "message": "Also inspect Docker logs on the next check."
}
```

User-to-AI messages are token-scoped. If `runtime_id` is set, the note is consumed only by matching target profile runtime responses. If `session_id` is also set, it is consumed only by MCP responses attached to that exact persistent console session. Generic notes can omit both `runtime_id` and `session_id`.

Unread AI-to-user messages contribute to Console sidebar and connector list badge counts for connector targets. Opening the Messages drawer can mark matching messages as read.

## Audit

```txt
GET /api/audit-logs
GET /api/audit-logs/{id}
```

Returns paginated audit events. Optional filters include `q`, `actor`,
`runtime_id`, `connector_kind`, and `target_id`. Audit action and payload search
use SQLCipher-backed FTS4 indexes; server, connector target, connector kind,
and token names remain regular filtered fields:

```txt
GET /api/audit-logs?limit=50&offset=0&q=docker&actor=mcp&runtime_id=3
```

List responses use the same pagination envelope as History and include a payload preview. `GET /api/audit-logs/{id}` returns the full payload.

Token create/revoke, permission changes, security settings changes, retention cleanup, console lifecycle/input, MCP execution states, and approval decisions are written.

Secret payloads, SSH private keys, and token values must not be written to audit logs.

Command text is stored in audit/history records. Users should avoid putting secret values directly in command strings and should be cautious when printing files or environment output that may contain secrets.
