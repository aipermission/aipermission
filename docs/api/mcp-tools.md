# MCP Tools

The MCP surface is connector-first. AIPermission does not expose separate
product-specific MCP wrappers for SSH, database, cache, queue, or container
products. SSH, Postgres, Redis, RabbitMQ, Docker, Kubernetes, and future
integrations are reached through the same connector target/action pipeline.

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
redis:8:3
rabbitmq:9:4
docker:10:5
kubernetes:11:6
```

The profile chooses which stored credential is used. The connector action still
runs locally through the gateway; AIPermission does not host a remote connector
service.

Clients should discover targets and actions at runtime. Do not hardcode SSH,
Postgres, Redis, RabbitMQ, Docker, or Kubernetes as special MCP modes; future
connector kinds use the same tools and `target_ref` shape.

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

Endpoint metadata is off by default. When the local user enables
`Expose server endpoint metadata to MCP` in Security settings, SSH refs may
include:

```json
{
  "metadata": {
    "host": "10.0.0.12",
    "port": 22,
    "username": "root"
  }
}
```

This is for clearer operator reasons only. MCP discovery still omits private
keys, reusable tokens, encrypted secrets, SSH key ids, and raw credential
payloads.

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

Postgres actions include schema/table inspection and bounded read-only queries.
Postgres managed database users are created from the local UI through credential
provisioning, which uses an admin profile to create a scoped role with a random
password and stores the resulting profile in the encrypted local vault.
Postgres backup/restore is also a local UI operator flow, not an MCP action.

Redis actions include bounded key scanning, key inspection, string writes, TTL
updates, and explicit key deletes.

RabbitMQ actions include overview metadata, visible vhost listing, bounded
queue listing, queue detail reads, binding listing, and bounded message peeking
with `ack_requeue_true`, plus explicit `publish_message` writes. Message
payload previews and published payloads may contain secrets or customer data;
prefer approval-required access until the workflow is trusted.

Docker actions include version metadata, scoped container/image/network/volume
listing, redacted container inspect metadata, bounded container log tails,
scoped `container_exec`, and explicit start/stop/restart lifecycle actions.
Docker profiles can be scoped to all containers, selected names/IDs, or name
patterns. If a token is bound to a profile that allows one container, MCP can
only list or operate on that container through Docker actions; image, network,
volume reads, `container_exec`, and live container console sessions are bounded
to the selected container scope where practical. Arbitrary host-level Docker
commands, prune, removal, and raw Docker command execution are not exposed.

Kubernetes actions include cluster version metadata, namespace/workload/pod/
service/ingress/node/event listing, resource JSON describes, bounded pod log
tails, and explicit `rollout_restart` for deployments. Kubernetes profiles can
scope access by namespace visibility. Raw `kubectl`, manifest apply/edit/delete,
pod deletion, scaling, and Secret value browsing are not exposed.

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
