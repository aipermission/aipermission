# MCP Tools

The MCP surface is connector-first. AIPermission does not expose separate
product-specific MCP wrappers for SSH or database products. SSH, Postgres, and
future integrations are reached through the same connector target/action
pipeline.

Recommended package use:

```bash
npx -y @aipermission/mcp init
```

The bridge connects to the backend through `AIPERMISSION_API_URL` and
`AIPERMISSION_API_TOKEN`. The URL must be a local gateway origin using
`localhost`, `127.0.0.1`, or `[::1]`; remote URLs are rejected before the token
is sent.

## Tool List

```txt
list_connector_targets()
get_connector_help(target_ref)
get_connector_actions(target_ref)
call_connector_action(target_ref, action_name, input?, reason?)
get_connector_action_request(request_id)
```

## Connector Model

Every connector uses the same permission path:

1. target
2. credential profile
3. connector action
4. token action permission
5. approval or direct execution
6. history
7. audit

The `target_ref` format is:

```txt
<connector_kind>:<target_id>:<profile_id>
```

Examples:

```txt
ssh:3:1
postgres:7:2
```

The profile chooses which stored credential is used. The connector action still
runs locally through the gateway; AIPermission does not host a remote connector
service.

## list_connector_targets

Returns target/profile refs visible to the current token.

Example response:

```json
[
  {
    "target_ref": "ssh:3:1",
    "target_id": 3,
    "target_name": "core-1",
    "connector_kind": "ssh",
    "profile_id": 1,
    "profile_label": "admin",
    "profile_kind": "private_key",
    "actions": [
      { "name": "exec", "execution_rule": "approval_required" },
      { "name": "read_console", "execution_rule": "always_run", "expires_at": "2026-06-11T12:30:00Z" }
    ],
    "hints": [
      "Use get_connector_help and get_connector_actions before calling connector actions for the first time."
    ]
  }
]
```

Visibility is permission-scoped, not a live health check. A visible SSH target
may still be powered off, unreachable, reject authentication, or require host
key review. Treat action execution errors as the current reachability signal.

## get_connector_help

Returns connector-specific operator guidance for one `target_ref`. Call it
before using a connector kind for the first time in a session.

## get_connector_actions

Returns the action list for one `target_ref`.

SSH actions currently include:

```txt
exec
read_console
restart_console_session
browse_remote_files
start_file_download
```

Postgres actions currently include read-oriented metadata/query actions such as
schema/table inspection and bounded read-only queries.

## call_connector_action

Creates or runs one connector action according to the token permission rule.

Example SSH command:

```json
{
  "target_ref": "ssh:3:1",
  "action_name": "exec",
  "input": {
    "command": "systemctl is-active docker"
  },
  "reason": "Check Docker service state before cleanup."
}
```

Example response:

```json
{
  "status": "completed",
  "request_id": 42,
  "target_ref": "ssh:3:1",
  "target_name": "core-1",
  "connector_kind": "ssh",
  "profile_label": "admin",
  "action_name": "exec",
  "display_text": "active\n",
  "output": {
    "exit_code": 0,
    "stdout": "active\n"
  }
}
```

## approval_pending

`approval_pending` is not terminal. Poll the connector action request.

```json
{
  "status": "approval_pending",
  "request_id": 43,
  "target_ref": "ssh:3:1",
  "connector_kind": "ssh",
  "action_name": "exec",
  "retry_after_seconds": 3,
  "assistant_hint": "Wait 3 seconds, then poll this connector action request until it is completed, failed, declined, stale, or blocked."
}
```

When the operator clicks Run, the gateway checks approval context drift before
execution. If the token, permission, target/profile public metadata, connector
kind/version, action definition, or action payload changes before approval, the
request becomes `stale` and the AI must submit a fresh action request.

## running

Long SSH commands can outlive the initial MCP call. Poll the connector action
request until terminal.

```json
{
  "status": "running",
  "request_id": 44,
  "target_ref": "ssh:3:1",
  "connector_kind": "ssh",
  "action_name": "exec",
  "retry_after_seconds": 3,
  "assistant_hint": "Wait 3 seconds, then call get_connector_action_request again. For SSH exec actions, inspect live output with the read_console connector action before sending another long-running command to the same target. If the action appears stuck, use the restart_console_session connector action for that target."
}
```

For SSH live output, call:

```json
{
  "target_ref": "ssh:3:1",
  "action_name": "read_console",
  "input": { "tail_bytes": 20000 },
  "reason": "Inspect live output for the running command."
}
```

If the persistent SSH console appears stuck and the operator approves recovery,
call:

```json
{
  "target_ref": "ssh:3:1",
  "action_name": "restart_console_session",
  "reason": "Recover a stuck persistent SSH console session."
}
```

## blocked

`blocked` prevents execution:

```json
{
  "status": "blocked",
  "target_ref": "ssh:3:1",
  "connector_kind": "ssh",
  "action_name": "exec",
  "error": "Connector action is blocked for this token"
}
```

## get_connector_action_request

Reads one connector action request by id. The request must belong to the current
MCP token.

Terminal statuses:

```txt
completed
failed
declined
blocked
error
stale
```

## SSH Notes

SSH `exec` is for non-interactive commands. Use bounded output commands such as:

```sh
journalctl --no-pager -u SERVICE -n 100
docker logs --tail 100 NAME
tail -n 100 /path/to/log
```

Avoid commands that wait for interactive stdin. Use the web console for
interactive work.

MCP connector responses never include file contents, gateway temporary paths,
archive staging paths, or local upload contents.
