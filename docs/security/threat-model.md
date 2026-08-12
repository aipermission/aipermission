# Threat Model

AIPermission is a local, single-user developer gateway. Its security model is intentionally narrower than a remote DevOps platform.

## Protected Assets

- connector credentials stored by the gateway, including SSH private keys and
  database/API secrets
- API tokens used by MCP clients
- encrypted SQLite database files
- database password
- gateway vault secret
- connector action history, SSH transcripts, messages, and audit records
- connector targets reachable from configured credential profiles
- Project Vault values and metadata

## Trust Boundary

Supported:

```txt
developer machine -> localhost gateway -> configured connector targets
```

Unsupported:

```txt
LAN users -> shared gateway
internet users -> public gateway
team members -> central hosted gateway
```

The localhost bind is the primary network boundary. Host-header checks, CORS, UI session cookies, and CSRF are defense in depth for local browser use. They do not make LAN or public exposure safe.

## Main Risks

### Local Hostile Web Content

Risk: a malicious page or browser extension attempts to call the local UI API.

Mitigations:

- local-only RemoteAddr and Host checks
- CORS allowlist
- HttpOnly UI session cookie after unlock
- SameSite Strict cookies
- CSRF header/cookie pair for mutating web REST requests
- loopback-only CORS origin validation; configured origins cannot point at non-loopback hosts

UI session and CSRF cookies use `Secure` and `SameSite=Strict` for defense in
depth on the supported local `localhost` gateway URL. HTTPS reverse-proxy/LAN
deployment is unsupported and must not be used to reinterpret these cookies as
remote authentication.

Browser extension boundary:

A malicious browser extension with broad host/page permissions is treated as a compromised local client. AIPermission can reduce ordinary web-page and CSRF risk, but it cannot guarantee protection from an extension that can observe pages, inject actions, read visible UI content, or issue privileged localhost requests from the user's browser profile. Use a trusted browser/profile for AIPermission and avoid untrusted extensions while the gateway is unlocked.

### Token Leakage

Risk: an MCP token is committed to a project config or copied into an unsafe place.

Mitigations:

- token values are shown once by default
- reusable token copy is opt-in
- project-local MCP config files are added to `.git/info/exclude` by the MCP init command when possible
- tokens can be revoked from the UI
- tokens can expire automatically when created for temporary access
- token action permissions can expire automatically for temporary maintenance
  windows
- MCP auth uses SHA256 hashes of high-entropy random tokens, not stored reusable token payloads

### SSH Host Impersonation

Risk: first SSH connection reaches an attacker-controlled host.

Mitigations:

- first unknown host key returns a SHA256 fingerprint approval prompt
- the user should verify the fingerprint through a trusted channel
- approved keys are pinned in the local `known_hosts` file outside the encrypted database
- later host key mismatches are rejected
- replacing a pinned peer key is serialized with secret delivery; it revokes
  active Vault session leases, closes those secret-bearing sessions, and stales
  pending Vault environment-session approvals before the trust file changes

### Secrets In Connector Input Or Output

Risk: connector action input, command text, action output, approval notes,
console transcripts, messages, or audit records include secrets.

Mitigations:

- basic redaction is enabled by default for common token, password, API-key, bearer-token, and private-key patterns before connector action history, console transcripts, messages, and audit payloads are persisted or returned through MCP
- structured connector output is converted to bounded canonical JSON before
  recursive redaction, so typed values and custom JSON marshalers cannot bypass
  traversal; encrypted history and MCP reuse the same redacted projection
- Security can add custom regex redaction rules on top of the built-in basic rules
- approval execution uses a separate encrypted raw action payload so redaction cannot change the connector action that runs after approval
- approval-required connector actions store an approval-context snapshot and become
  `stale` if token permission, token validity, target/profile context, SSH key
  fingerprint, MCP tool metadata, connector kind/version, action definition, or
  action payload hash changes before Run
- docs and approval dialogs warn that connector input, command text, output, notes, transcripts, and audit payloads may be persisted
- operator instructions tell agents to avoid printing secrets
- users can prefer existence checks and redacted output commands
- output projection failures after remote execution become `outcome_unknown`
  so agents do not automatically repeat a possibly completed side effect

Known risk: redaction is best-effort and pattern-based, including custom regex rules. It is not a guarantee that every secret shape will be detected. Do not print secrets into connector action input, console output, command text, reasons, or messages.

### Project Vault Session Exfiltration

Risk: an approved command or process in a session that received Project Vault
values sends them to another service, writes them to disk, or passes them to a
detached child process.

Mitigations:

- MCP never receives raw values, generated values, or environment envelopes
- generation and session application require explicit Prompt or Always project
  capabilities; Disabled is the default
- Prompt approval lists exact item assignments without displaying values;
  Always is an explicit autonomous secret-use grant
- approval context binds item revisions, project visibility, target/profile,
  runtime, and exact session identity
- session environment transport is framed, acknowledged, and omitted from
  persistent command/transcript setup records
- item, binding, token, permission, and project-scope changes invalidate
  affected pending requests and exact-session leases
- connector target/profile changes and MCP stop are serialized against secret
  delivery, close affected secret-bearing sessions, and stale pending requests
- MCP command/input authorization and the corresponding PTY write share that
  lifecycle gate, preventing permission mutations from crossing the
  authorize-to-I/O boundary; long-running output is reauthorized before return
- a PTY write that acquires the lifecycle gate before a permission or trust
  mutation may finish that write; the mutation then revokes and closes the
  session before any later token operation
- in-memory leases expire after at most 12 hours and do not survive restart;
  expiry ends agent authorization for the exact session but does not prove that
  the remote shell or detached child processes erased inherited values

Known risk: once the local user approves applying a value, code in that remote
process can use or exfiltrate it. Closing the interactive shell cannot guarantee
that detached children discarded inherited environment values. Redaction is not
an execution sandbox.

### Shared Persistent Console Runtime

Risk: two MCP tokens or the local operator use the same non-secret persistent
console runtime and observe work started by another principal.

This is intentional for the local-only, single-user product model. A normal
connector console is one shared live workspace inside the currently unlocked
database runtime, not a multi-tenant terminal. Token target/profile/action
permissions still gate how an MCP client reaches that runtime. Secret-bearing
Project Vault sessions are the exception: MCP execution and observation require
an exact token-bound lease containing the session id, generation, approval
context, and environment-content hash. Local-UI-created Vault sessions remain
human-console-only unless an MCP token creates its own approved session.

Do not treat AIPermission runtime principals as operating-system users or team
tenant isolation. Use separate local databases when workflows require a hard
workspace boundary.

### SSH Shell-Interpreted Commands

Risk: a user approves command text without noticing how the target shell will interpret it.

SSH connector `exec` actions run as shell command bodies on the configured
server. Shell operators such as `;`, `&&`, pipes, redirects, command
substitution, and globs are interpreted by the remote shell. This is the
intended SSH connector execution model, not a separate injection bug. The
operator should approve SSH `exec` commands as shell scripts, not as inert
strings.

Mitigations:

- approval dialogs show the exact command body
- command text is stored for history/audit
- operator instructions tell agents to prefer readable, non-interactive command bodies
- users can copy approval command text for external review before running it

### Process Memory And Local State

Risk: a local process compromise reads backend memory or restarts the gateway to clear counters.

UI sessions and auth rate-limit counters are in-memory process state. Restarting the backend clears them. Database passwords are present in process memory while unlock, import, or password-change requests are handled. This is accepted within the local single-user trust boundary; AIPermission does not claim protection against a compromised developer machine.

### `always_run` Misuse

Risk: a token/connector target/action grant runs without human approval.

Mitigations:

- permissions are explicit per token, connector target, credential profile, and action
- `approval_required` is the recommended default for real systems
- `always_run` should be temporary and scoped to trusted maintenance windows
- revoke tokens or remove permissions when work is done

### Hostile Mail Content

Risk: an email subject or body contains prompt injection that tells an AI agent
to disclose secrets, invoke another connector, run commands, or send a reply.

Mitigations:

- Mail content is documented and presented as untrusted external data
- read actions use IMAP peek semantics and do not silently mark messages read
- read/unread, move, archive, delete, send, and reply are explicit actions with
  independent token rules
- incoming HTML is converted to safe text; remote resources and active content
  are not loaded
- attachment content download is not exposed by the initial connector
- outgoing formatted content is allowlist-sanitized before approval and again
  before execution
- recipient-domain policy can constrain outbound delivery
- operator instructions prohibit cross-connector actions based solely on mail
  instructions and prohibit automatic retry after `submission_unknown`

Known risk: an AI granted Always access can still reason over hostile message
content and choose an allowed action. Prompt is recommended for message bodies,
outbound mail, and mailbox mutations until the workflow is narrowly understood.
Redaction does not turn message content into trusted instructions.

### Cross-Project Token Discovery

Risk: a token configured for one local project discovers or invokes connector
targets organized under another project.

Mitigations:

- every connector target belongs to one project
- token project visibility is checked before target/profile/action grants
- hidden projects are omitted from MCP target discovery
- direct connector action calls are rejected when the target project is hidden
- approval snapshots include project identity and become stale after project or
  project-scope drift
- history and audit records snapshot project identity when activity is created

Projects are not a remote multi-user isolation claim. They narrow local MCP
token visibility for one developer; the localhost and single-user boundaries
remain unchanged.

## Out Of Scope

- remote/LAN hosting
- team/RBAC web auth
- enterprise policy management
- automatic command risk scoring
- guaranteed detection of every possible secret format
- guaranteed protection against a compromised local machine
- guaranteed protection against malicious browser extensions installed in the active browser profile
