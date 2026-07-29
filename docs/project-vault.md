# Project Vault

Project Vault is a local, project-scoped secret inventory for one developer.
It stores reusable secret values in the encrypted AIPermission database and can
apply selected values to supported connector console sessions without returning
those values to MCP.

Project Vault is not a hosted secret manager, team vault, remote sync service,
or unrestricted sandbox. The local-only and single-user product boundaries
still apply.

## Stored Item Model

Each item has:

- a recommended environment-style name such as `GITHUB_PROJECT_PAT`
- one owner project and optional shared project visibility
- secret type, provider, environment, description, tags, and usage notes
- optional expiration and warning window
- value and metadata revisions for optimistic concurrency
- generated or user-supplied value source metadata

Values are stored inside SQLCipher and are also encrypted with the gateway
vault using workspace/item/revision-bound associated data. List and detail
responses contain metadata only. Reveal is an explicit, audited local UI
operation and is never an MCP response.

The local UI can replace an existing value by importing one or by running a
built-in generator. A generated value is shown in an explicit, audited local
preview backed by a five-minute encrypted token. Regeneration discards the
previous preview, and the value is stored only after local confirmation.
Replacement updates the value revision and source metadata atomically,
invalidates affected sessions, and never returns a generated value in the
replacement response. It does not rotate the provider-side credential.

## Project Capabilities

MCP access is independent from connector action grants. A token needs an
effective project capability:

- `vault.metadata.read`: list non-secret item metadata; this capability may be
  configured as `always_run`
- `vault.item.generate`: request generation of a new secret; Prompt or Always
- `vault.session.apply`: request a console-session restart with selected
  environment assignments; Prompt or Always

Project visibility is checked independently for the target project and every
selected source project. Cross-project application is intentional when the
same token can access both projects; an item does not need to be shared with
the target project. This supports an operator deliberately applying one
project's credential to another project target without making default bindings
into permission grants. Capability changes, project visibility changes, item
mutation, binding mutation, token revocation, or token expiry invalidate
affected pending requests and exact-session leases.

Disabled capabilities have no stored grant and cannot be called. Prompt creates
a local approval request. Always executes immediately through the same
idempotency, context-drift, history, audit, and secret-redaction path without
opening the approval dialog. Always is an explicit autonomous secret-use grant,
not a way to expose the secret value to MCP.

## Session Application

Supported connector runtimes advertise a typed session-environment capability.
The UI can preselect default bindings or let the local user choose item
assignments before starting a console session.

A Vault environment session started from the local UI is a human-console
session. It does not grant an MCP token permission to continue in that
secret-bearing shell. An agent must use
`restart_session_with_environment` through its own token capability and
connector action permission so AIPermission can create the exact token-bound
session lease.

For MCP, `restart_session_with_environment` is explicit:

1. The agent requests exact item ids, source projects, target/profile, and
   assignment behavior.
2. AIPermission creates a tracked request. Prompt requests expire after 15
   minutes; Always requests execute immediately.
3. For Prompt, the local user reviews names and metadata, never values. Always
   skips the dialog only because the user explicitly granted autonomous use.
4. Run revalidates the token, project capabilities, item revisions, target,
   profile, runtime, and current session identity.
5. The exact old session is closed and a new session receives a framed,
   one-time environment envelope. When replacing an active session, its current
   terminal rows and columns are preserved.
6. The gateway waits for an acknowledgment before considering the
   environment applied.
7. The resulting authorization is bound to the token, runtime, session id,
   generation, approval-context hash, and environment-content hash.
8. Replacing a pinned connector peer key is serialized with secret delivery.
   Before the trust file changes, AIPermission revokes every active Vault
   session lease, closes those sessions, and marks pending environment-session
   approvals stale. The gateway-level trust store is shared, so this
   intentionally favors a broad fail-closed boundary over retaining unrelated
   secret-bearing sessions.
9. Connector target/profile edits, token/project permission changes, and MCP
   stop are serialized against secret delivery. They revoke affected leases,
   close active secret-bearing sessions, and stale pending requests before the
   changed context can be reused.
10. MCP console input and command delivery hold the same lifecycle gate from
    exact-session authorization through the PTY write. Permission or trust
    mutations therefore cannot cross the authorize-to-I/O boundary; output
    observation is reauthorized after long-running waits.

The lifecycle gate defines the concurrency order. A PTY write that acquired the
gate before a permission or trust mutation may finish that write; the mutation
then revokes the lease and closes the session before any later token operation.

Values are not written into command text, approval payloads, history, audit,
MCP responses, or persistent transcript setup lines. Environment values live in
the remote process after application; in-memory gateway leases last at most 12
hours and do not survive a gateway restart. Persisted lease rows are lifecycle
records for revocation and review; they never restore authorization after a
restart. Lease expiry ends the agent's authorization to observe or operate the
exact session. It does not prove that the remote shell or a detached child
process erased an inherited value.

Session history stores an immutable metadata snapshot of each applied item,
including its name, source project, and revisions. Deleting the live Vault item
does not erase that non-secret historical context.

Stored text values may be short, but session injection requires at least 12
bytes. Exact-value output redaction for very short strings would create unsafe
false positives and corrupt ordinary console output.

One session environment accepts at most 32 items, 16 KiB per value, and 128 KiB
in total. Environment names use uppercase letters, digits, and underscores.
Shell/runtime control names such as `PATH`, `HOME`, `NODE_OPTIONS`,
`GIT_SSH_COMMAND`, and the `LD_`/`DYLD_` families are reserved and cannot be
injected. SSH targets configured with a forced shell command do not advertise
Vault session-environment support.

Default bindings are available only for connector profiles that advertise the
typed session-environment capability. They are convenience selections, not
permission grants. A local user may override the selection when starting a
session. Saving an identical binding or permission/scope configuration is a
no-op and does not revoke an unchanged active session. The overwrite option
changes an existing environment value only inside the newly started shell; it
does not edit remote files, shell profiles, or provider-side credentials.

## MCP Tools

```text
list_vault_items(project_ref?)
call_vault_action(project_ref, action_name, input, reason, idempotency_key)
get_vault_action_request(request_id)
cancel_vault_action_request(request_id)
```

`call_vault_action` currently supports:

- `generate_item`
- `restart_session_with_environment`

Every mutation creates a tracked request. Prompt requests can be declined,
canceled by the owning token, expire after 15 minutes, or become stale when the
approval context changes. Always requests execute immediately through the same
context validation and tracking path. New request creation is bounded per local
workspace/token; retrying an existing idempotency key does not consume another
request slot. MCP output contains metadata and status only. Vault action input
is strictly normalized to the selected action schema before persistence;
`usage_notes` accepts only `location` and `notes`, and session items accept only
`item_id`, `source_project_id`, and optional `replace_existing`. Undeclared
fields are rejected rather than echoed into the approval lifecycle.

Local reveal and generated-preview endpoints are separately rate limited to
eight and ten requests per minute. Generated previews are encrypted,
single-current-preview tokens that expire after five minutes; the local dialog
also clears displayed plaintext after 30 seconds.

## Security Boundary

Project Vault reduces secret copying and accidental disclosure. It cannot make
an approved shell command harmless. Code running in a session that receives a
secret can use it for its intended service or send it elsewhere. Redaction is
defense in depth, not an exfiltration boundary.

Use least-privilege, short-lived service credentials and narrow project/token
capabilities. Do not inject broad personal credentials into sessions that do not
need them. Detached child processes may retain inherited environment values
after the parent shell closes, so treat session application as granting the
remote process access to those secrets.

See also:

- [Credential Boundary](security/credential-boundary.md)
- [Threat Model](security/threat-model.md)
- [Storage Encryption](security/storage-encryption.md)
- [MCP Permission Flow](architecture/mcp-permission-flow.md)
