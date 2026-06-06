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
read_console(server_id, tail?)
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
    "hints": [
      "Use non-interactive apt commands.",
      "Prefer journalctl --no-pager for service logs."
    ]
  }
]
```

By default, `list_servers` hides endpoint metadata and returns only `id`, `name`, `description`, `execution_rule`, and `hints`. Security can enable **Expose endpoint metadata to MCP** if an operator wants MCP clients to also receive `host`, `port`, and `username`.

`hints` contains gateway-generated, safe operational notes that help an AI agent produce better shell commands for that server. The current MVP does not accept custom per-server hints through the server CRUD API. Hints must not contain credentials.

If the same API token exists in more than one unlocked local database, authentication returns a conflict instead of guessing which workspace to use.

## exec

Creates a remote command execution request.

`exec` is for non-interactive commands. The gateway runs MCP commands with stdin closed to the command body so tools such as `cat`, `read`, or interactive installers cannot consume the internal shell wrapper and hide the exit marker. Use the web console for truly interactive work, or pass all required input through flags/files inside a single command.

Input:

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
4. execution rule
5. global MCP Started/Stopped runtime state

If MCP execution is stopped in the web UI, `exec` returns:

```json
{
  "status": "stopped",
  "error": "MCP execution is stopped in the local gateway. Start MCP from the web UI before running commands."
}
```

### always_run

`always_run` commands run through the backend-owned persistent console session. If the web console is attached to that session, AI/MCP commands are visible in the same transcript.

For MCP client timeout safety, the backend waits for a bounded period in a single `exec` call. If the command is still running, the response is `running`; the shell session continues and output keeps streaming to the web console. The AI should call `read_console(server_id)` and poll `get_request(request_id)` before sending another long-running command to that server.

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
  "assistant_hint": "Wait 3 seconds, then call get_request again. Use read_console to inspect live output before sending another command to this server."
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

`approval_pending` is not terminal. The AI should follow `assistant_hint` and poll `get_request` until the request reaches a terminal status.

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

## read_console

Returns the latest persistent console transcript for a visible server when that token has `always_run` permission for the server. Active sessions are preferred over closed sessions.

Console visibility is server-scoped, not token-private. In the local single-user model, if two tokens both have `always_run` permission for the same server, either token can read the shared persistent console transcript for that server. Use separate servers/databases or keep permissions temporary if token-level transcript isolation is required.

For `approval_required` tokens, use `get_request(request_id)` to inspect approved command state and final output. This prevents approval-only tokens from reading unrelated manual console transcripts that may contain sensitive output.

Input:

```json
{
  "server_id": 3,
  "tail": 12000
}
```

Example response:

```json
{
  "status": "connected",
  "server_id": 3,
  "session_id": 12,
  "transcript": "apt-get update...\n"
}
```

`status` is the persistent console session status, such as `connected`, `closed`, `error`, or `none`; command request state should be read from `get_request`.

The transcript tail is truncated on UTF-8 boundaries.

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
- `browse_remote_files`, `start_file_download`, `save_file_download`,
  `upload_files`, `pause_file_transfer_batch`, `resume_file_transfer_batch`,
  and `cancel_file_transfer_batch` require `always_run` permission for that
  server.
- `approval_required` transfer approval is not implemented yet. Use the local UI
  when a human should make the transfer decision.
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
