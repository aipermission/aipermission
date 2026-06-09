# MCP Permission Flow

An MCP client connects to the gateway with an API token. The AI assistant never sees SSH credentials; it only sees the limited MCP tool surface exposed by the local gateway.

The local UI also has a global MCP Started/Stopped switch for the active
database runtime. Saved token/server permissions stay in the database, but new
MCP command execution is blocked while the runtime is stopped. This prevents a
gateway restart from automatically making old `always_run` grants live again.

Current MCP tools:

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
```

For the detailed tool contract, see [MCP Tools](../api/mcp-tools.md).

## list_servers

`list_servers()` returns only the servers that the token can access. Temporary
permission grants include `expires_at`; expired grants are omitted.

Example:

```json
[
  {
    "id": 3,
    "name": "core-1",
    "execution_rule": "approval_required",
    "expires_at": "2026-06-07T14:00:00Z"
  }
]
```

## exec

Example call:

```txt
exec(3, "ls", "Inspect the current directory")
```

Gateway flow:

1. Validate the API token.
2. Reject revoked tokens.
3. Check whether the token has permission for server `3`.
4. Reject expired token/server permission grants.
5. Read the execution rule.
6. Check whether the global MCP runtime is started.
7. If the runtime is stopped, return `stopped`.
8. If the rule is `always_run`, run the command directly.
9. If the rule is `approval_required`, create a pending approval.
10. If the rule is `blocked`, reject the command without execution.
11. Return the result or a follow-up `request_id` to the MCP client.

## Approval Required

Approval-required MCP requests are non-blocking. The gateway stores the command request as `pending_approval` and returns `approval_pending` plus `request_id`. The user decides in the web UI:

- Run
- Decline
- Add a note

When a pending approval is created, the gateway snapshots the approval context:
token identity, token/server permission, server profile, SSH key fingerprint,
MCP tool metadata, and command payload hash. When the user clicks Run, the
gateway recomputes that context before execution. If it changed, the request
becomes `stale` and the AI must submit a fresh command.

When the user runs a non-stale request, the backend executes the command in the target server's persistent console session. If the operator entered a note while clicking Run, the gateway delivers that note to the matching MCP token through the message queue. The AI follows progress with `get_request(request_id)`. `read_console(server_id)` and `read_console(server_ids, tail)` are reserved for tokens with `always_run` permission so approval-only tokens cannot read unrelated manual console transcripts.

When the user declines the request, the decline note is stored as `user_note` on that command request and returned by `get_request`.

The live message queue is separate. A user can send a message from the Console UI; the gateway injects it as `user_note` into the next matching MCP response.

For credential rules, see [Credential Boundary](../security/credential-boundary.md).
