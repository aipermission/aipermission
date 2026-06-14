# Architecture Decisions

This note records product-level decisions that should stay stable across the
first public RC. Use it as a compact entry point before changing security,
storage, or MCP behavior.

Detailed ADR files:

- [ADR 0001: Local-Only Gateway](../adr/0001-local-only.md)
- [ADR 0002: No Cloud Mode](../adr/0002-no-cloud-mode.md)
- [ADR 0003: Single-User Design](../adr/0003-single-user-design.md)
- [ADR 0004: SQLCipher Choice](../adr/0004-sqlcipher-choice.md)

## ADR-001: Local-Only Gateway

Decision: AIPermission is a localhost-only developer gateway.

Reason:

- The web REST API is designed for one local user after database unlock.
- Remote servers are SSH targets, not places where the gateway is hosted for other users.
- Docker Compose publishes only `127.0.0.1`, and the backend refuses non-loopback bind addresses.
- LAN/public exposure would require a different auth, CSRF, session, and threat model.

Consequence:

- Remote/LAN gateway hosting is unsupported.
- Host-header and origin checks are defense in depth, not a remote security boundary.
- Documentation must continue to say "do not expose this service on a LAN or the internet."

## ADR-002: SQLCipher Database With Vaulted Secrets

Decision: The local database is SQLCipher-encrypted, and secret payloads are also encrypted through the gateway vault layer.

Reason:

- A downloaded `.aipdb` backup should be portable across developer machines.
- The database password protects the full local workspace.
- The vault layer separates secret payload handling from normal table fields.

Consequence:

- The database password is unrecoverable.
- The vault is not a second independent security boundary if the encrypted database and password are both compromised.
- Secret payloads must not be returned by REST or MCP responses.

## ADR-003: SSH Host Key Trust Is Local Machine State

Decision: SSH host key pins live in the local `known_hosts` file under the data path, not inside each named database.

Reason:

- Host key trust belongs to the developer's local machine.
- Sharing host pins across named databases avoids repeated fingerprint prompts for the same SSH target.
- `.aipdb` backup/import should move credentials and settings, not silently move host trust decisions.

Consequence:

- Named databases are not separate SSH host-trust sandboxes.
- Importing a database on another machine may require approving host fingerprints again.

## ADR-004: MCP Tokens Are Scoped Runtime Bearer Credentials

Decision: MCP clients authenticate with API tokens, and token target/profile/action permissions determine visible connector actions.

Reason:

- AI clients should never receive SSH private keys or passwords.
- Different AI clients or projects can use different tokens.
- The gateway can revoke tokens or change execution rules without changing SSH credentials.

Consequence:

- Tokens are bearer credentials and must be treated as sensitive.
- The default token value model is show-once.
- Duplicate token matches across multiple unlocked databases are rejected instead of guessed.

## ADR-005: MCP Execution Has A Global Runtime Switch

Decision: Saved permissions do not automatically mean live execution is enabled. Each unlocked runtime has a global MCP Started/Stopped state.

Reason:

- A developer may leave `always_run` permissions configured for a project but not want MCP execution live immediately after startup.
- The safest default is stopped unless the user explicitly enables automatic start for that database.
- Start/Stop should not delete token action permission configuration.

Consequence:

- New unlocks start with MCP execution stopped unless Security enables automatic start.

## ADR-006: Pending Approvals Are Bound To Their Approval Context

Decision: A pending MCP connector-action approval is valid only for the context
captured when it was created.

Reason:

- A user approves a concrete connector action for a concrete token, target,
  credential profile, and permission state, not a reusable command pattern.
- Token permission, token validity, target/profile config, SSH key, MCP tool
  metadata, or command payload changes can make an old approval misleading.

Consequence:

- `approval_required` requests store an approval-context snapshot and hash.
- If that context drifts before Run, the request becomes `stale` and must be
  requested again.
- `always_run` still evaluates current permission state at execution time; it
  does not create reusable approval-pattern records.
- The sidebar controls the runtime Started/Stopped state.
- Stopped MCP execution blocks new command execution while preserving saved permissions.
