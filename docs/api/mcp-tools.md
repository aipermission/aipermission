# MCP Tools

The MVP MCP surface stays small and focused on scoped command execution,
operator messages, console reads, and explicit file transfer workflows.

Recommended package use:

```bash
npx -y @aipermission/mcp init
```

The bridge connects to the backend through `AIPERMISSION_API_URL` and `AIPERMISSION_API_TOKEN`. The URL must be a local gateway origin using `localhost`, `127.0.0.1`, or `[::1]`; remote URLs are rejected before the token is sent.

## Tool List

```txt
list_servers()
exec(server_id, command, reason?)
exec(server_ids, command, reason)
read_console(server_id, tail?)
read_console(server_ids, tail?)
restart_console_session(server_id)
get_request(request_id)
list_requests(status?)
send_message(message, server_id?, session_id?)
list_file_transfers(server_id?, direction?, status?, limit?, offset?)
get_file_transfer(transfer_id)
list_file_transfer_batches(server_id?, direction?, status?, limit?, offset?)
get_file_transfer_batch(batch_id)
browse_remote_files(server_id, path?)
start_file_download(server_id, remote_paths, archive_name?)
save_file_download(batch_id, local_path, overwrite?)
upload_files(server_id, local_paths, remote_dir, overwrite?)
pause_file_transfer_batch(batch_id)
resume_file_transfer_batch(batch_id)
cancel_file_transfer_batch(batch_id)
```

Future SQL tools such as `list_databases()` and `query(database_id, sql, reason?)` are not implemented in the current MVP.

## list_servers

Returns only servers visible to the current token.

Example response:

```json
[
  {
    "id": 3,
    "name": "core-1",
    "description": "Kubernetes control-plane maintenance access.",
    "execution_rule": "approval_required",
    "expires_at": "2026-06-07T14:00:00Z",
    "hints": [
      "Use non-interactive apt commands.",
      "Prefer journalctl --no-pager for service logs."
    ]
  }
]
```

By default, `list_servers` hides endpoint metadata and returns only `id`,
`name`, `description`, `execution_rule`, optional `expires_at`, and `hints`.
Security can enable **Expose endpoint metadata to MCP** if an operator wants MCP
clients to also receive `host`, `port`, and `username`.

`list_servers` is permission-scoped, not a live SSH health check. Agents should
try `exec` when they need to use a server and treat dial, timeout, SSH
authentication, and host-key errors as the current reachability signal.

`expires_at` appears when the token/server permission is temporary. Expired
permissions are omitted from `list_servers` and do not authorize `exec`,
`read_console`, or file-transfer tools.

`hints` contains gateway-generated, safe operational notes that help an AI agent produce better shell commands for that server. The current MVP does not accept custom per-server hints through the server CRUD API. Hints must not contain credentials.

If the same API token exists in more than one unlocked local database, authentication returns a conflict instead of guessing which workspace to use.

## exec

Creates a remote command execution request.

`exec` is for non-interactive commands. The gateway runs MCP commands with stdin closed to the command body so tools such as `cat`, `read`, or interactive installers cannot consume the internal shell wrapper and hide the exit marker. Use the web console for truly interactive work, or pass all required input through flags/files inside a single command.

Single-server input:

```json
{
  "server_id": 3,
  "command": "ls -la",
  "reason": "Inspect the current directory"
}
```

The gateway checks:

1. token validity
2. token revocation
3. token permission for the server
4. token/server permission expiration
5. execution rule
6. global MCP Started/Stopped runtime state

Responses and command history can include `policy_warnings` for common
high-risk command patterns such as destructive file operations, service/package
changes, cluster/container changes, firewall/network changes, disk operations,
or likely credential reads. These warnings are best-effort UX safety rails; they
do not replace token permissions, approval review, or operator judgment.

If MCP execution is stopped in the web UI, `exec` returns:

```json
{
  "status": "stopped",
  "error": "MCP execution is stopped in the local gateway. Start MCP from the web UI before running commands."
}
```

### always_run

`always_run` commands run through the backend-owned persistent console session. If the web console is attached to that session, AI/MCP commands are visible in the same transcript.

For MCP client timeout safety, the backend waits for a bounded period in a
single `exec` call. If the command is still running, the response is
`running`; the shell session continues and output keeps streaming to the web
console. The AI should poll `get_request(request_id)`, use
`read_console(server_id)` to inspect live output when the token has
`always_run`, use `read_console(server_ids, tail)` after multi-server commands,
and avoid sending another long-running command to that server until the active
request reaches a terminal status. If the request remains
running and `read_console` shows no useful progress, the recovery path is
`restart_console_session(server_id)`.

Example completed response:

```json
{
  "status": "completed",
  "server_id": 3,
  "session_id": 12,
  "exit_code": 0,
  "stdout": "README.md\n",
  "stderr": ""
}
```

Example running response:

```json
{
  "status": "running",
  "request_id": 42,
  "server_id": 3,
  "session_id": 12,
  "retry_after_seconds": 3,
  "assistant_hint": "Wait 3 seconds, then call get_request again. Use read_console to inspect live output before sending another command to this server. If the request remains running and the console shows no useful progress, use restart_console_session(server_id) to recover the persistent console session."
}
```

### approval_required

`approval_required` requests are stored as `pending_approval` and shown in the Console UI. The MCP response is non-blocking:

```json
{
  "status": "approval_pending",
  "request_id": 39,
  "retry_after_seconds": 3,
  "assistant_hint": "Wait 3 seconds, then call get_request. Continue polling get_request until terminal status."
}
```

`approval_pending` is not terminal. The AI should follow `assistant_hint` and
poll `get_request` until the request reaches a terminal status. When the
operator clicks Run, the gateway first checks that the SSH-backed console
session can become ready; offline hosts, refused ports, authentication failures,
and host-key failures become terminal request errors instead of silent approval
successes.

When a pending approval is created, the gateway records an approval-context
snapshot covering the token, token/server permission, server profile, SSH key
fingerprint, MCP tool metadata, and command payload hash. If that context
changes before the operator clicks Run, the request becomes `stale`; the AI
should submit a fresh `exec` request with the current context.

UI behavior:

- The Console sidebar item shows the total pending approval count.
- The Console server list shows each server's pending count.
- If the selected server has a pending request, the approval dialog opens automatically.
- If the dialog is closed without a decision, badges keep showing the pending request.

### blocked

`blocked` prevents execution:

```json
{
  "status": "blocked",
  "error": "token is blocked for this server"
}
```

### Multi-Server Mode

The same `exec` tool can run one command across multiple visible servers by
using `server_ids` instead of `server_id`.

Input:

```json
{
  "server_ids": [3, 4, 5],
  "command": "apt update",
  "reason": "Refresh package indexes before maintenance"
}
```

Multi-server `exec` applies the same permission model as single-server `exec`,
but independently per target:

- `always_run`: creates a command request and starts it in that server's
  persistent console session
- `approval_required`: creates a separate pending approval for that server
- `blocked` or unauthorized: skips that target without creating a command
  request

The command `reason` is required for bulk execution so every created request has
auditable context.

Example response:

```json
{
  "status": "accepted",
  "command": "apt update",
  "parallelism": 3,
  "retry_after_seconds": 3,
  "assistant_hint": "Each target has its own request_id when execution or approval started. Poll get_request(request_id) for running or approval_pending items. Approval-required targets wait for the local operator; blocked targets were skipped.",
  "items": [
    {
      "status": "running",
      "request_id": 101,
      "server_id": 3,
      "server_name": "worker-1",
      "execution_rule": "always_run"
    },
    {
      "status": "pending_approval",
      "request_id": 102,
      "server_id": 4,
      "server_name": "worker-2",
      "execution_rule": "approval_required",
      "approval_context_hash": "sha256:..."
    },
    {
      "status": "blocked",
      "server_id": 5,
      "execution_rule": "blocked",
      "error": "This token is blocked from executing commands on this server"
    }
  ]
}
```

The AI should poll each returned `request_id` with `get_request`. Do not assume
all targets started just because the top-level response is `accepted`; inspect
each item.

## read_console

Returns the latest persistent console transcript for one visible server, or for multiple visible servers after a multi-server `exec`, when that token has `always_run` permission for each server. Active sessions are preferred over closed sessions.

Console visibility is server-scoped, not token-private. In the local single-user model, if two tokens both have `always_run` permission for the same server, either token can read the shared persistent console transcript for that server. Use separate servers/databases or keep permissions temporary if token-level transcript isolation is required.

For `approval_required` tokens, use `get_request(request_id)` to inspect approved command state and final output. This prevents approval-only tokens from reading unrelated manual console transcripts that may contain sensitive output.

Single-server input:

```json
{
  "server_id": 3,
  "tail": 12000
}
```

Single-server response:

```json
{
  "status": "connected",
  "server_id": 3,
  "session_id": 12,
  "transcript": "apt-get update...\n"
}
```

Multi-server input:

```json
{
  "server_ids": [3, 4, 5],
  "tail": 12000
}
```

Multi-server response:

```json
{
  "status": "ok",
  "items": [
    {
      "status": "connected",
      "server_id": 3,
      "server_name": "core-1",
      "session_id": 12,
      "transcript": "apt-get update...\n"
    },
    {
      "status": "none",
      "server_id": 4,
      "server_name": "worker-2"
    },
    {
      "status": "blocked",
      "server_id": 5,
      "error": "read_console requires always_run permission for this server; use get_request to inspect approval_required command results"
    }
  ]
}
```

`status` is the persistent console session status, such as `connected`, `closed`, `error`, or `none`; command request state should be read from `get_request`.

The transcript tail is truncated on UTF-8 boundaries.

## restart_console_session

Restarts the backend-owned persistent console session for a visible server.
This is a recovery tool for cases where the console shell appears stuck,
the AI sees a request running for too long, or `read_console` shows no useful
progress.

`restart_console_session` does not run a remote shell command. It closes the
current gateway-owned console session for the server, marks any running command
requests for that server as `error`, writes an audit event, and lets the next
`exec` call open a fresh SSH session.

Input:

```json
{
  "server_id": 3
}
```

Example response:

```json
{
  "status": "restarted",
  "server_id": 3,
  "server_name": "core-1",
  "closed_session_ids": [12],
  "canceled_running_requests": 1,
  "assistant_hint": "The persistent console session was closed. The next exec call for this server will open a fresh SSH session."
}
```

The tool requires a non-blocked token/server permission and the global MCP
runtime must be Started. Because persistent console sessions are server-scoped,
restart is also server-scoped, not token-private.

## get_request

Returns one command request owned by the current token.

Example response:

```json
{
  "id": 39,
  "status": "completed",
  "server_id": 3,
  "session_id": 12,
  "exit_code": 0,
  "stdout": "ok\n",
  "stderr": ""
}
```

Terminal statuses:

```txt
completed
failed
declined
error
stale
```

Non-terminal statuses:

```txt
pending_approval
running
```

If a request is declined, the response includes `user_note` when the operator provided one.

`blocked` is returned directly by `exec` when the token is not allowed for a server. No command request is created for blocked execution, so `get_request` will not later return a `blocked` record.

## list_requests

Lists recent command requests for the token. Optional `status` examples:

```txt
pending_approval
running
completed
failed
declined
error
stale
```

MCP request tools return only MCP-origin command requests for the calling token.
They do not expose manual Console History rows.

## send_message

Allows the AI to write a short note to the web Console Messages panel. It is a coordination channel, not a place for credentials or large shell output.

Input:

```json
{
  "server_id": 3,
  "session_id": 12,
  "message": "Docker install started; waiting for package installation to finish."
}
```

User-to-AI messages are created from the Console Messages drawer. The gateway stores them by token and optionally server/session scope. Session-scoped notes are delivered only to MCP responses attached to that exact persistent console session. The next matching MCP `exec`, `read_console`, or `get_request` response can include the message as `user_note`.

AI-to-user unread messages are included in Console badge counts and are displayed in the Messages drawer.

## File Transfer Tools

MCP file transfer tools are token-scoped and server-scoped. They use the same
server permission table as command execution, but transfer management is stricter
than read-only status:

- `list_file_transfers`, `get_file_transfer`, `list_file_transfer_batches`, and
  `get_file_transfer_batch` return sanitized metadata for servers visible to the
  token.
- `start_file_download` and `upload_files` start immediately with `always_run`
  permission. With `approval_required`, they create a `pending_approval` queue
  in the local Transfer Center; the operator can approve selected files and
  reject the rest with a note.
- `browse_remote_files`, `save_file_download`, `pause_file_transfer_batch`,
  `resume_file_transfer_batch`, and `cancel_file_transfer_batch` require
  `always_run` permission for that server.
- MCP can upload and download only explicit local paths requested through the
  local MCP process. Tool responses never include file contents, local temporary
  paths, local upload contents, or gateway temporary/archive paths.

`list_file_transfers` and `list_file_transfer_batches` support optional
`server_id`, `direction`, `status`, `limit`, and `offset` filters:

```json
{
  "server_id": 3,
  "direction": "download",
  "status": "running",
  "limit": 20
}
```

`get_file_transfer_batch` returns per-file progress for a queue:

```json
{
  "id": 12,
  "status": "running",
  "total_items": 3,
  "completed_items": 1,
  "transferred_bytes": 10485760,
  "bytes_per_second": 5242880,
  "eta_seconds": 8,
  "items": [
    {
      "id": 41,
      "status": "completed",
      "remote_path": "/var/log/syslog",
      "file_name": "syslog"
    }
  ]
}
```

`browse_remote_files` lists one remote directory through SFTP:

```json
{
  "server_id": 3,
  "path": "/var/log"
}
```

`start_file_download` starts a queued remote download:

```json
{
  "server_id": 3,
  "remote_paths": ["/var/log/syslog", "/var/log/auth.log"],
  "archive_name": "logs.zip"
}
```

The response includes a batch record plus `retry_after_seconds` and
`assistant_hint`. The AI should poll `get_file_transfer_batch` for progress and
then call `save_file_download` if the operator asked to write the completed file
or archive to a local path.

`save_file_download` writes a completed MCP-started download batch to a local
file or directory path. It refuses to overwrite an existing local file unless
`overwrite` is true:

```json
{
  "batch_id": 12,
  "local_path": "/home/user/Desktop/logs.zip",
  "overwrite": false
}
```

`upload_files` uploads explicit local files through the local MCP package to one
remote directory:

```json
{
  "server_id": 3,
  "local_paths": ["/home/user/Desktop/app.env"],
  "remote_dir": "/home/deploy",
  "overwrite": false
}
```

The response includes a batch record plus `retry_after_seconds` and
`assistant_hint`. The AI should poll `get_file_transfer_batch` for progress.

Pause, resume, and cancel operate on active transfer queues:

```json
{
  "batch_id": 12
}
```

## Output Normalization

MCP command outputs are normalized to plain text for AI consumption. The web console transcript remains raw PTY output for the terminal experience.

## Operator Instructions

Agent behavior is standardized in [aipermission Operator Skill](../skills/aipermission-operator/SKILL.md). It covers polling, live console reads, command reasons, and secret hygiene.
