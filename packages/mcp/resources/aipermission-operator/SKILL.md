---
name: aipermission-operator
description: Use when operating targets through the AIPermission MCP gateway. Guides AI agents to discover connector targets, call connector actions, handle approval_pending/running states, write short reasons, avoid leaking secrets, and keep execution auditable.
---

# AIPermission Operator

## Core Rule

Use AIPermission as a local, developer-controlled permission gateway.

You are allowed to operate only the connector targets returned by
`list_connector_targets()`. Do not ask for SSH passwords, private keys, database
passwords, API keys, or raw credentials. The gateway owns credentials,
permissions, approvals, runtime sessions, and audit history.

AIPermission is not a hosted DevOps control plane. Treat it as a temporary,
scoped maintenance/debugging channel controlled by the human operator.

## Discovery

Before acting:

1. Call `list_connector_targets()`.
2. Pick the relevant `target_ref`.
3. Call `get_connector_help(target_ref)` the first time you use that connector.
4. Call `get_connector_actions(target_ref)` and choose the narrowest action.
5. Call `call_connector_action(target_ref, action_name, input, reason)`.

If no target is visible, say that the current token has no accessible connector
targets. If an action grant includes `expires_at`, treat access as temporary and
finish within that maintenance window or ask the operator to extend access.

Target visibility is permission-scoped, not a live health check. A visible SSH
target may still be powered off, unreachable, reject authentication, or require
host-key review. A visible database/API target may still reject the credential
profile. Treat action errors as the current reachability/authorization signal.

## Reasons

Every `call_connector_action` should include a short `reason`.

Good reasons:

```text
Check Docker service state before cleanup.
Inspect recent kubelet errors on worker node.
List Postgres schemas before a read-only metadata query.
Peek a RabbitMQ queue after the operator approved payload inspection. Publish
RabbitMQ messages only when the operator explicitly asked for a write.
```

Avoid vague reasons:

```text
run command
debug
test
```

## Approval Flow

`approval_pending` is not terminal.

When `call_connector_action` returns `approval_pending`:

1. Read `retry_after_seconds`; default to 3 seconds if missing.
2. Wait that long.
3. Call `get_connector_action_request(request_id)`.
4. Continue polling until the status is terminal.

Terminal statuses:

```text
completed
failed
declined
blocked
error
stale
```

If the request is `declined`, read the note/error and follow the user's
correction. If the request is `stale`, the approval context changed before
execution; send a fresh `call_connector_action` request with the current target,
action, input, and reason.

## Running Flow

When `call_connector_action` or `get_connector_action_request` returns
`running`:

1. Poll `get_connector_action_request(request_id)` every 3-5 seconds.
2. Do not start another long-running action on the same target/profile until the
   active request reaches a terminal status, unless the user explicitly asks.
3. For SSH `exec`, use the SSH connector's `read_console` action when live
   output is needed and the token has permission for it.
4. If an SSH request appears stuck for an unusually long time, ask the operator
   before recovery unless they already asked you to recover. When approved, call
   the SSH connector's `restart_console_session` action for the same target_ref.

## SSH Practice

For SSH connector `exec`, prefer commands that are:

- non-interactive
- bounded in output
- explicit about destructive actions
- easy to audit from history

Use examples like:

```sh
systemctl is-active docker
journalctl --no-pager -u k3s-agent -n 100
docker logs --tail 100 CONTAINER
kubectl get nodes -o wide
df -h
free -m
```

Avoid huge unbounded output:

```sh
cat huge.log
journalctl
docker logs NAME
```

For apt on Debian/Ubuntu:

```sh
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y PACKAGE
```

After install/uninstall checks in the same shell, refresh command lookup:

```sh
hash -r 2>/dev/null || true
```

## File Transfer

Use connector file-transfer actions only when the user explicitly asks to move
files or inspect remote paths. Prefer the smallest explicit path set. Do not use
globs, recursive copy, or directory transfer unless a connector action
explicitly supports that behavior.

MCP connector responses never include file contents, gateway temp paths, archive
staging paths, or local upload contents.

## Secret Hygiene

Command text, action input, action output, history, audit records, and console
transcripts may be stored in the encrypted local database. Avoid printing
secrets. Prefer checking whether a file/key exists before reading credential
files or environment files.

If a secret appears in output, do not repeat it. Summarize the finding and ask
the operator how to rotate or redact it.
