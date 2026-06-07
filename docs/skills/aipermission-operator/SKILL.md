---
name: aipermission-operator
description: Use when operating servers through the AIPermission MCP gateway. Guides AI agents to handle approval_pending/running states, poll get_request, read live console output, write short reasons, avoid leaking secrets, and keep command execution safe and auditable.
---

# AIPermission Operator

## Core Rule

Use AIPermission as a local, developer-controlled execution gateway.

You are allowed to operate only the servers returned by `list_servers()`. Do not ask for SSH passwords, private keys, database passwords, or raw credentials. The gateway owns credentials, permissions, approvals, console sessions, and audit history.

AIPermission is not a general DevOps control plane. Treat it as a temporary, scoped maintenance/debugging channel controlled by the human operator.

## Initial Discovery

Before executing commands:

1. Call `list_servers()`.
2. Read each server's `name`, `id`, `execution_rule`, optional `expires_at`,
   and `hints`.
3. Use the numeric `id` returned by the tool.
4. Pick the narrowest server set that can answer the task.

If no server is visible, say that the current token has no accessible servers.
If `expires_at` is present, treat access as temporary. Finish within that
maintenance window or ask the operator to extend access.

## Command Reasons

Every `exec` call should include a short `reason`.

Good reasons:

```text
Check Docker service state before cleanup.
Inspect recent kubelet errors on worker node.
Verify trial worker deployment after node label change.
```

Avoid vague reasons:

```text
run command
debug
test
```

## Approval Flow

`approval_pending` is not terminal.

When `exec` returns `approval_pending`:

1. Read `retry_after_seconds`; default to 3 seconds if missing.
2. Wait that long.
3. Call `get_request(request_id)`.
4. Continue polling until the status is terminal.
5. If status becomes `running`, keep polling `get_request(request_id)`; `read_console` is only available when the token has `always_run` permission for that server.

Terminal statuses:

```text
completed
failed
declined
blocked
error
stale
```

If the request is `declined`, read `user_note` and follow the user's correction.
If the request is `stale`, the approval context changed before execution; send a fresh `exec` request with the current command and reason instead of trying to reuse the old approval.

## Running Flow

When `exec` or `get_request` returns `running`:

1. If the server permission is `always_run`, call `read_console(server_id)` before sending another long-running command to the same server.
2. Poll `get_request(request_id)` every 3-5 seconds.
3. Use `read_console(server_id)` between polls only when the token has `always_run` permission.
4. Do not start another long-running command on the same server until the active request reaches a terminal status, unless the user explicitly asks.

If the request appears stuck for an unusually long time and `read_console`
shows no useful progress, ask the operator before recovery. When the operator
agrees, call `restart_console_session(server_id)`. This closes the
gateway-owned persistent console session and the next `exec` opens a fresh SSH
session. Treat any canceled request as inconclusive and re-run only the minimal
safe inspection needed.

## Message Flow

Use `send_message(message, server_id?, session_id?)` for short operator-visible notes.

Good messages:

```text
Docker install started; waiting for package installation to finish.
K3s agent joined. Checking node labels now.
The command is still running; reading console output before next step.
```

When a response includes `user_note`, treat it as live operator guidance. Apply it before continuing.

## File Transfer Flow

File transfer tools use the target server permission. `always_run` starts the
queue immediately. `approval_required` creates a local approval queue in
AIPermission Transfer Center; wait for the operator to approve selected files or
reject the queue with a note.

Use them only when the user explicitly asks to move files or inspect transfer
state. Prefer the smallest explicit path set. Do not use globs, recursive copy,
or directory transfer unless a future tool explicitly supports those behaviors.

For remote-to-local downloads:

1. Call `start_file_download(server_id, remote_paths, archive_name?)`.
2. Poll `get_file_transfer_batch(batch_id)` until the batch is terminal.
   If the status is `pending_approval`, wait and poll again after the operator
   decides in the local UI.
3. If completed and the user asked for a local copy, call
   `save_file_download(batch_id, local_path, overwrite?)` with an explicit local
   destination.
4. Report the saved path and status metadata. Do not print file contents unless
   the user explicitly asks you to inspect the saved file.

For local-to-remote uploads, call `upload_files(server_id, local_paths,
remote_dir, overwrite?)` only with explicit local paths supplied by the user or
clearly located in the current local workspace. Poll `get_file_transfer_batch`
for progress.

The local AIPermission UI shows active and recent transfer queues in Transfer
Center. The operator can pause, resume, or cancel queues there.

## Safe Shell Practice

Prefer commands that are:

- non-interactive
- bounded in output
- explicit about destructive actions
- easy to audit from history

MCP `exec` closes stdin for the command body. Do not use commands that wait for interactive stdin. Use flags such as `-y`, `--no-pager`, heredoc-created files, or the manual web console for interactive work.

Use examples like:

```sh
systemctl is-active docker
journalctl --no-pager -u k3s-agent -n 100
docker logs --tail 100 CONTAINER
kubectl get nodes -o wide
df -h
free -m
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

For package verification, prefer installed-state checks over ambiguous removed-package residue:

```sh
dpkg-query -W -f='${db:Status-Abbrev} ${binary:Package} ${Version}\n' docker-ce docker-ce-cli 2>/dev/null | grep '^ii'
```

## Output Hygiene

Avoid huge unbounded output.

Prefer:

```sh
tail -n 100 /path/to/log
journalctl --no-pager -n 100
docker logs --tail 100 NAME
kubectl logs --tail=100 POD
```

Avoid:

```sh
cat huge.log
journalctl
docker logs NAME
```

## Secret Hygiene

Command text, command output, history, audit records, and console transcript may be stored in the encrypted local database.

Do not print secrets unless the user explicitly asks and accepts the risk. Avoid commands that dump:

- private keys
- `.env` files
- token files
- database passwords
- cloud credentials
- Kubernetes secrets

Prefer existence and metadata checks:

```sh
test -f /path/to/.env && echo exists
ls -l /path/to/secret-file
kubectl get secret NAME -o jsonpath='{.metadata.name}'
```

## Destructive Actions

Before destructive actions:

1. Inspect current state.
2. Explain the exact destructive command in the `reason`.
3. Prefer one clear destructive step at a time.
4. Verify after completion.

Examples of destructive actions:

- deleting containers, volumes, images
- uninstalling packages
- removing files
- restarting critical services
- changing Kubernetes labels or draining nodes

## Multi-Server Work

For multiple servers:

1. Check visibility with `list_servers()`.
2. Work one risky operation at a time.
3. Keep each command targeted to one server unless the user explicitly requests batch behavior.
4. Summarize per-server status after each phase.

## Final Response

When reporting back to the user, include:

- servers touched
- commands or command groups run
- important findings
- changes made
- verification results
- any pending/recommended next steps

Keep it concise and operational.
