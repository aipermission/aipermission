# MVP Scope

The first public MVP should stay small, usable, and honest about its boundaries.

Main target:

> After `docker compose up`, a user can create credentials, add connector targets, create API tokens, grant token/target/profile/action permissions, connect an MCP client, call `list_connector_targets`, and run actions through `call_connector_action`. Actions either run directly or wait for approval according to their execution rule.

## Product Boundary

The MVP is not a DevOps platform. It does not own production operations. It gives a developer a controlled, token-scoped, auditable execution channel for debugging, maintenance, incident triage, and temporary AI-assisted automation.

The MVP does not introduce first-class management modules for every external
system. It introduces one connector pipeline. SSH, Postgres, Redis, RabbitMQ, and future
integrations are connector kinds that provide their own actions while sharing
the same target/profile/action permission model. If an allowed SSH target has
the needed CLI tools and access, the AI can operate at command level through
the SSH connector `exec` action. If a structured connector such as Postgres is
selected, the AI uses that connector's smaller purpose-built action surface.

The gateway itself is local-only. It is not designed to run on a remote server for browser/MCP clients, to be shared on a LAN, or to act as a central team control plane.

## Included

- local Docker runtime
- React web dashboard
- Go backend
- SQLCipher SQLite storage
- encrypted credential vault
- named local databases
- backup/import with `.aipdb`
- connector target management
- connector credential profile management
- connector target/profile/action permission pipeline
- built-in SSH connector with gateway-generated `ed25519` and `rsa` keypairs,
  imported SSH keys, public key install command, persistent console sessions,
  SFTP transfer, and host-key approval
- built-in Postgres connector with schema/table inspection and bounded
  read-only query actions
- API token creation and revocation
- token-to-target/profile/action permissions
- execution rules: `always_run`, `approval_required`, `blocked`
- MCP connector target discovery
- MCP connector action execution
- approval dialog and request polling
- Run / Decline / note
- live message queue
- connector action history
- audit event storage
- operator instructions for AI clients

## Deferred

- advanced command risk analysis
- advanced Postgres/query policy outside the local connector pipeline
- SQL parser and masking
- stronger manual terminal command event parsing for structured history

For detailed sequencing, see [Implementation Roadmap](implementation-roadmap.md) and [Roadmap](../ROADMAP.md).
