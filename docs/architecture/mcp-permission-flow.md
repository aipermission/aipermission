# MCP Permission Flow

An MCP client connects to the gateway with an API token. The AI assistant never
sees SSH keys, database passwords, API keys, or raw connector credentials. It
only sees connector targets and connector actions allowed by the token.

The local UI has a global MCP Started/Stopped switch for the active database
runtime. Saved connector permissions stay in the encrypted database, but new MCP
connector action execution is blocked while the runtime is stopped. This
prevents a gateway restart from automatically making old `always_run` grants
live again.

Current MCP tools:

```txt
list_connector_targets()
get_connector_help(target_ref)
get_connector_actions(target_ref)
call_connector_action(target_ref, action_name, input?, reason?)
get_connector_action_request(request_id)
```

For the detailed tool contract, see [MCP Tools](../api/mcp-tools.md).

## Target Discovery

`list_connector_targets()` returns only target/profile refs that the token can
use. Temporary grants include `expires_at`; expired grants are omitted.

Example:

```json
[
  {
    "target_ref": "ssh:3:1",
    "target_name": "core-1",
    "connector_kind": "ssh",
    "profile_label": "admin",
    "actions": [
      { "name": "exec", "execution_rule": "approval_required" }
    ]
  }
]
```

Target visibility is not a live health check. A visible SSH target may still be
powered off, unreachable, reject authentication, or require host-key review.
Execution errors are the current reachability signal.

## Action Execution

Example call:

```json
{
  "target_ref": "ssh:3:1",
  "action_name": "exec",
  "input": { "command": "ls" },
  "reason": "Inspect the current directory."
}
```

Gateway flow:

1. Validate the API token.
2. Reject revoked or expired tokens.
3. Resolve the connector target and credential profile.
4. Prepare the connector action.
5. Check token permission for target/profile/action.
6. Reject expired action grants.
7. Check whether the global MCP runtime is started.
8. If the runtime is stopped, return a stopped/error response.
9. If the rule is `always_run`, execute the connector action.
10. If the rule is `approval_required`, create a pending connector action approval.
11. If the rule is `blocked`, reject the action without execution.
12. Record history and audit events.

## Approval Required

Approval-required MCP requests are non-blocking. The gateway stores the
connector action request as `approval_pending` and returns `request_id`. The
user decides in the web UI:

- Run
- Decline
- Add a note

When a pending approval is created, the gateway snapshots the approval context:
token identity, connector target, credential profile, token action permission,
connector metadata, action input, and prepared payload hash. When the user
clicks Run, the gateway recomputes that context before execution. If it
changed, the request becomes `stale` and the AI must submit a fresh connector
action request.

When the user runs a non-stale request, the backend executes the connector
action through the same connector runtime used by `always_run`. The AI follows
progress with `get_connector_action_request(request_id)`.

When the user declines the request, the decline note is stored on the connector
action request and returned to the MCP client.

For credential rules, see [Credential Boundary](../security/credential-boundary.md).
