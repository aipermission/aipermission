# SSH Key Model

The preferred SSH model is Dokploy-style key bootstrap.

aipermission does not collect VPS SSH passwords. The gateway can generate SSH
keypairs or explicitly import an existing private key. In both cases, private
keys are stored in the local encrypted vault and the public key/install command
is shown to the user.

## Key Types

Generated key types:

- `ed25519`
- `rsa`

The recommended default is `ed25519`.

Imported keys support common OpenSSH private key formats that the backend can
parse, including ed25519, rsa, and ecdsa keys. Imported RSA keys must be at
least 2048 bits. Passphrase-protected imports use the passphrase only during
import; the passphrase is not saved.

Key-file reads are local drafts, not imports. Selecting another file, replacing
the pasted key, changing generation/import mode, or closing the editor retires
the earlier read. Import waits for the selected file to finish reading; only
the currently visible draft is submitted. A failed read shows a bounded error
without exposing file contents.

## User Flow

1. The user opens the Credentials page.
2. The user creates an `ed25519` or `rsa` key, or imports an existing private key.
3. The gateway stores the private key in the encrypted vault.
4. The gateway shows the public key and install command.
5. The user runs the install command on the VPS from their own terminal.
6. The user selects that SSH key when creating an SSH connector target.
7. The gateway opens SSH connections with that private key.
8. On the first connection, the gateway returns the remote host key fingerprint for explicit approval.
9. After approval, the remote host key is stored in the gateway `known_hosts` file.

The Connectors page can also import SSH host entries to prefill SSH connector
forms.
This imports host metadata only; it does not silently read or import private key
material referenced by `IdentityFile`. Wildcard-only blocks such as `Host *` are
used only as defaults for concrete hosts and are not shown as standalone server
entries. `ProxyCommand` is reported as configured, but the raw command is not
returned. In Docker, gateway config scan reads the container user's config;
choose a config file or paste config content to parse a local workstation config.

The importer is deliberately a bounded metadata prefill parser, not an OpenSSH
configuration evaluator. It accepts concrete `Host` aliases plus `Host *`
defaults and the first applicable `HostName`, `User`, `Port`, `IdentityFile`,
`ProxyJump`, and `ProxyCommand` presence value. It does not expand `Include`,
evaluate `Match`, apply wildcard or negated host patterns, expand `%` tokens, or
execute proxy commands. Unsupported directives are ignored. The selected file
is limited to 256 KiB and the operator must review every prefilled field before
creating a connector.

## Install Command

The gateway shows a command in this shape:

```txt
mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%s\n' 'ssh-ed25519 <PUBLIC_KEY> aipermission' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
```

The command appends the public key and does not overwrite the existing `authorized_keys` file.

## Server Uninstall

The SSH connector delete dialog offers:

- delete the local record only
- remove remote `authorized_keys` entries containing the selected gateway public key blob, then delete the local record

Remote cleanup matches the public key material, so comments or installed key
options do not change its identity. Profiles sharing the same remote user and
key material form one cleanup group, even when they reference different local
key records. The configured connection hostname is preserved for host-pin
verification; the durable identity uses a separate canonical hostname.

Before reading a private key or connecting, the connector validates every public
profile/group and persists cleanup intent in its scoped encrypted database
resource storage. A successful, exact removal response is durably confirmed;
an exact response proving the key is already absent is also confirmation, not
an error. Confirmed groups are not authenticated again when a later group or
local archival step fails.

Lost remote replies, cancellation and missing durable confirmation leave the
intent unresolved and keep the local target available. Retrying cannot silently
reconnect with that potentially revoked key. If confirmation committed but its
local response was lost, a subsequent verified journal read can recover that
confirmation and finish without connecting again. Changed target/profile/key/host-trust
snapshots likewise require operator reconciliation rather than reusing an old
confirmation. Missing profiles, invalid public keys and missing host trust are
reported separately as preflight failures; a rejected preflight creates no new
cleanup record, but earlier cleanup history remains. Connector-owned records contain public identity and
confirmation evidence, not private key material.
An unresolved record for the target also blocks remote cleanup after its old
profile is removed or its username/key is replaced. Current profile membership
cannot hide an unfinished historical cleanup. Completed retired groups are not
authenticated again, and unrelated targets retain their own history boundaries.

Historical reconciliation records explicit external absence evidence for every
distinct recorded host/port, remote user, key-material fingerprint and trusted
host-pin set, plus the selected location. DNS names and shared host pins are not
automatically treated as equivalent locations. Each subject requires an absence
assertion, an external verification method and a bounded explanation; verifying
only the replacement endpoint cannot resolve the original location. The original
key fingerprint can be displayed from its recorded material digest without
reading a private key or substituting a replacement key.

Evidence also carries the deletion-context digest and exact journal generation.
The operation owner must recompute/check the complete current public context
under lifecycle exclusion before submitting a decision. Every historical proof
prefix is validated independently, so a later complete proof cannot conceal an
earlier incomplete one. Journal format v2 rejects v1 records rather than silently
promoting an older current-endpoint assertion into complete historical evidence.
The journal never contacts a remote host to obtain that operator evidence.

Local authenticated target operations expose this evidence through the existing
generic `POST /api/connector-targets/{id}/operations/{operation}` route:

- `key-cleanup-status` accepts an empty object and returns the current
  `deletion_context_digest`, relevant journal entries and server-selected identity
  choices. Each choice includes the exact historical verification subjects.
  Retired target groups remain visible; shared current identities can also reveal
  unresolved history originating from another local target. Unrelated groups
  are not included. Removing the last profile does not hide this history or
  prevent exact historical attestation; remote deletion still requires a saved
  credential profile and its normal preflight checks.
- `key-cleanup-attest` requires the displayed `resource_id`, `generation`,
  `deletion_context_digest`, `identity_digest`, a bounded `reason` and `coverage`
  for every displayed subject. Each coverage entry supplies `subject_id`,
  `absent: true`, an external `method` (`provider_console`,
  `independent_admin_session` or `decommissioned_location`) and its own bounded
  explanation. These are operator assertions, not automated remote checks.

The generic lifecycle policy admits the mutation exclusively before obtaining
the fresh target/profile/public-key/trust snapshot, and holds admission through
response publication. Changed context, generation or identity is rejected with
409; no supplied identity replaces a server-selected choice. Inspection and
attestation never read a private key, open a shell or execute remote commands.
A successfully completed operation records `connector.key_cleanup_attested`
in the local human audit without creating an execution surface. Domain proof
and observation audit are separate writes; an audit failure reports uncertainty
after the proof may already have committed. A retired-group decision does not
confirm a replacement key/user.

If persistence or its reply is uncertain, reload `key-cleanup-status` and inspect
the recorded generation and proof. Do not automatically resend the mutation or
treat a 409 as proof that nothing committed. These local human management
operations are not added to the AI/MCP action catalog.

In **Connectors**, the SSH row's **Reconcile key cleanup** operation opens the
public evidence dialog, even when no execution profile remains. Select the
record and server-provided identity, then verify every displayed historical
location independently. Each location requires its own verification method,
explanation and explicit absence confirmation, followed by a decision reason.
Changing the record or identity clears entered evidence. This records a human
assertion; it does not contact the host or repair `authorized_keys` for you.

**Recorded cleanup evidence** retains the original public identity and each
previous decision, including its full per-location proof. Expand a decision to
inspect or copy its JSON independently of the empty new-decision fields. The
selected identity label describes the current server-selected endpoint, not the
first historical location. An acknowledgement must match that complete identity
and the submitted context and proof before the dialog reports it as confirmed.

The dialog disables another submission, refresh and close while a decision is
pending. Afterwards it reloads current evidence and clears all entered proof.
A conflict, invalid acknowledgement or lost reply is shown as an unconfirmed
outcome, never automatically retried as a new mutation. An acknowledged decision
whose subsequent observation fails remains distinct from an unconfirmed write;
use **Reload cleanup evidence** and inspect the generation and audit before
another decision. A target or database change retires the old request and
requires freshly loaded evidence, not a cached confirmation.

The cleanup writes a private temporary file in the same `.ssh` directory and
atomically replaces `authorized_keys` only after the complete filtered file is
ready. It rejects symlinked or non-owned key paths rather than risking a
partial rewrite. If the remote shell lacks the required file tools or the
replacement fails, the original key file and local target remain unchanged.
Only the key type and blob fields identify a removal candidate; matching text
inside a comment or quoted option does not. The option scanner follows OpenSSH's
quote-escape and space/tab boundaries, preserving CRLF lines that are retained.
Unclosed field quotes, interior field carriage returns, or a matching candidate with
unsupported option syntax stop publication without confirming absence. This
syntax check is not a replacement for OpenSSH's authorization-option policy
validation; ambiguous cleanup requires external verification and reconciliation.

## Security Boundary

Responses may show:

- public key
- fingerprint
- install command
- key name
- key type

Responses must not show:

- private key
- vault encryption secret
- decrypted secret payloads

A connector target record does not contain decrypted credentials. The built-in
SSH connector references gateway-managed key material by `ssh_key_id`.

Some SSH targets, especially NAS appliances, show an interactive menu before a
normal shell. SSH connector targets can store optional advanced startup behavior
for that compatibility case: startup input sent after connect, or a forced shell
command. These values are not credentials. Keep them empty for normal Linux
targets and avoid putting secrets in them because console transcripts and audit
metadata can include shell output.

## Host Key Verification

Gateway SSH clients use explicit first-connect fingerprint approval:

- The first unknown host key returns `unknown_ssh_host_key` with the host, key type, SHA256 fingerprint, and public host key payload.
- The user should verify the fingerprint through a trusted channel such as the VPS provider console or their own trusted terminal.
- After approval, the host key is stored under the local data path `known_hosts` file. This file is outside the encrypted database and stores host key pins only, not SSH private keys.
- Initial trust and replacement both validate a staged file, sync its contents,
  atomically publish it and sync the parent directory before reporting success.
  Publication failure preserves the prior trust state when rollback succeeds;
  an unconfirmed rollback reports indeterminate trust instead of success.
- Later connections for the same host/port verify the host key.
- If the host key changes, the connection is rejected.

If the server was intentionally rebuilt or rotated, the user must deliberately remove the related `known_hosts` entry.

This reduces MITM risk without breaking the passwordless SSH key workflow. Approval is still a trust decision: if the first fingerprint is approved without verifying it elsewhere, a first-connection MITM can still be trusted by mistake.
