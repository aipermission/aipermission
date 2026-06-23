# What Is aipermission?

Related central notes:

- [Docs Index](index.md)
- [Local Gateway](architecture/local-gateway.md)
- [MCP Permission Flow](architecture/mcp-permission-flow.md)
- [Credential Boundary](security/credential-boundary.md)
- [MVP Scope](mvp/scope.md)
- [Use Cases](mvp/use-cases.md)

`aipermission` is a local developer gateway that lets AI coding assistants
operate on connector targets without receiving SSH private keys, SSH passwords,
database credentials, API credentials, or other connector secrets.

The current model ships with SSH, Postgres, Redis, RabbitMQ, and Docker
connectors. SSH provides live terminal/file-transfer actions, Postgres provides
structured metadata and bounded read-only query actions, Redis provides bounded
key browsing plus explicit write/delete actions, RabbitMQ provides queue
metadata, bindings, bounded message previews, and explicit message publishing,
and Docker provides scoped container inventory, logs, redacted inspect metadata,
and explicit lifecycle actions. They use the same target, credential profile,
token permission, approval, history, and audit pipeline.

The product is intentionally not positioned as a full DevOps platform.

> Give the AI controlled hands, not your credentials.

## Design Decision: Local Developer Gateway Only

AIPermission is designed to run on the developer's own machine.

Remote systems and databases are connector targets reached from the local
gateway. They are not places where the gateway is meant to be hosted for other
users. The supported shape is:

```txt
developer machine -> local Docker gateway -> configured connector targets
```

The unsupported shapes are:

```txt
LAN users -> shared gateway
internet users -> public gateway
team members -> central hosted gateway
```

This is intentional. The web UI and REST API rely on a localhost trust boundary. After database unlock, protected web REST calls also require a local HttpOnly browser session cookie, but that cookie is not a remote multi-user auth model. AIPermission does not provide the security model expected from a shared DevOps control plane.

The backend refuses non-loopback bind addresses such as `0.0.0.0`, and Docker Compose publishes only `127.0.0.1` by default.

## Problem

When a developer debugs a real system with an AI assistant, the assistant often wants to inspect several targets:

- "Run this on core-1."
- "Check Kubernetes state on control-1."
- "Inspect worker-3 logs."
- "Restart only the API container on this Docker host."
- "Check this readonly PostgreSQL table."

Without a tool like aipermission, the developer becomes a terminal operator:

1. SSH into a server.
2. Copy and run a command.
3. Copy the output back to the AI.
4. Repeat for every target and every action.

This is slow, tiring, and error-prone. Worse, it can tempt people to paste SSH keys, passwords, or database credentials into AI tools. aipermission exists to avoid that.

## Product Positioning

`aipermission` is a local access and permission gateway for developers using AI.

It is for:

- solo developers
- small teams
- founders running their own infrastructure
- freelance developers
- full-stack developers using Codex, Claude Code, Cursor, Windsurf, VS Code, Gemini CLI, or similar tools

The user grants temporary, scoped access to selected connector targets and actions. The AI calls the gateway through MCP. The gateway checks token validity, target/profile/action permission, and execution rule. It either runs the action, asks the user for approval, or blocks the request.

Credentials never leave the gateway.

Saved token action permissions are separate from the live MCP execution switch.
By default, each unlock starts with MCP execution stopped. The user starts MCP
from the sidebar when they are ready; Security can opt into automatic MCP start
for a database.

This model also works well with AI client instructions or skills. For example,
a developer can define a workflow like "check a new VPS before adding it to the
cluster", "inspect container health across allowed SSH targets", "describe a
readonly Postgres schema", "inspect Redis cache keys", or later "call this
internal API operation through a stored credential profile." aipermission
provides the execution layer without exposing credentials.

## Typical Flow

1. The developer starts aipermission with local Docker.
2. The developer opens the local web UI.
3. The developer creates a credential profile, such as an SSH key or Postgres readonly role.
4. The developer adds a connector target and binds it to a credential profile.
5. The developer creates an API token.
6. The developer grants that token access to selected target/profile/action combinations.
7. The MCP client connects to the gateway with that token.
8. The AI operates through the gateway.
9. The developer watches, approves, declines, or sends notes from the web UI.
10. When the work is done, the token can be revoked, permissions can be removed, the database can be locked, or Docker can be stopped.

## What It Is Not

For the MVP, aipermission is not:

- a full DevOps automation platform
- an infrastructure management panel
- a CI/CD product
- a permanent production control plane
- a gateway hosted on a VPS for network users
- a LAN-shared team service
- an agent installed on every server
- a tool that gives credentials to an AI assistant

It is a local, developer-controlled bridge for temporary AI-assisted debugging, maintenance, and investigation.

If a token has access to an SSH target such as `control-1`, and that target has
the required CLI tools and network access, the AI can use those tools through
the SSH `exec` action. For structured systems such as Postgres, connector
actions expose a smaller purpose-built surface. The gateway does not need to
own every external system; it provides the permission, approval, execution, and
audit layer.

## High-Level Architecture

```txt
AI coding assistant
Codex / Claude Code / Cursor / Windsurf / MCP client
        |
        | MCP request + API token
        v
Local Docker container
aipermission gateway
        |
        | auth + permission check + approval flow
        v
Connector target
SSH server / Postgres database / Redis cache / RabbitMQ broker / Docker host / future local integration
```

The AI assistant does not receive SSH credentials or database passwords.

The MCP client uses only the limited tool surface exposed by the gateway.

Gateway responsibilities:

- local encrypted credential storage
- API token management
- connector target/profile/action permission checks
- execution rule checks
- pending approval management
- user message queue
- command/action history
- connector runtimes
- audit events

## Local Runtime Model

The default runtime is local Docker:

```txt
docker compose up
```

The local Docker setup includes:

- Go backend API
- React web dashboard
- SQLite database encrypted with SQLCipher
- gateway vault encryption
- MCP bridge through the npm package

Remote deployment is not part of the MVP. The product is designed around local developer control because credentials, approvals, and unlock state stay on the developer's machine.

## Credential Model

Credentials are stored only inside the local gateway.

Examples:

- gateway-generated SSH private key
- explicitly imported SSH private key
- SSH username
- Postgres connection data
- database passwords

Rules:

- credentials are stored in the encrypted local SQLite database
- secret payloads are additionally encrypted by the gateway vault layer
- API tokens are masked in the UI
- credentials are never returned by MCP responses
- credentials are never shown to the AI assistant
- credentials are never embedded in prompts
- credentials are used only by the gateway while executing approved or permitted actions
- private key passphrases are used only during import and are not stored

aipermission should not ask the user for a VPS SSH password. The preferred model is Dokploy-style:

1. gateway generates an `ed25519` or `rsa` keypair
2. private key stays in the local encrypted vault
3. public key install command is shown to the user
4. user pastes that command on the server

Install command shape:

```txt
mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%s\n' 'ssh-ed25519 <PUBLIC_KEY> aipermission' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
```

An API token is not an SSH password. It represents gateway permission.

## API Token And Permission Model

The developer creates an API token in the web UI and grants it access to one or
more connector target/profile/action combinations:

```txt
token: cursor-maintenance-session

allowed connector targets:
- ssh:core-1/admin -> exec approval_required
- ssh:core-1/admin -> read_console always_run
- postgres:main-db/readonly -> query_readonly approval_required
- redis:cache/ops -> get_key approval_required
- docker:prod-docker/api-only -> container_logs approval_required
```

The AI assistant can see and use only the connector targets and actions allowed
by that token. For example, if the token can access five SSH targets, one
Postgres target, one Redis target, and one Docker profile scoped to a single
container, `list_connector_targets` returns only those target/profile refs.

If the same token exists in more than one unlocked database, MCP authentication returns a conflict. The gateway does not guess which workspace should receive the command.

## MCP Tool Surface

Current tools:

```txt
list_connector_targets()
get_connector_help(target_ref)
get_connector_actions(target_ref)
call_connector_action(target_ref, action_name, input?, reason?)
get_connector_action_request(request_id)
```

SSH, Postgres, Redis, RabbitMQ, Docker, and future integrations are exposed as
connector actions instead of separate product-specific MCP tools.

## Connector Action Flow

Example MCP call:

```json
{
  "target_ref": "ssh:3:1",
  "action_name": "exec",
  "input": { "command": "ls" },
  "reason": "Inspect the current directory."
}
```

Gateway steps:

1. Validate the API token.
2. Resolve the connector target and credential profile.
3. Check whether the token can run that connector action.
4. Load the required secret payload inside the gateway.
5. Check the token action execution rule.
6. Run directly, create pending approval, or block.
7. Return a result or a follow-up request id.

If the SSH target is named `core-1`, the AI may see:

```json
{
  "target_ref": "ssh:3:1",
  "target_name": "core-1",
  "connector_kind": "ssh",
  "profile_label": "admin"
}
```

The SSH credential for `core-1` never leaves the gateway. The same rule applies
to Postgres passwords and future connector secrets.

If the global MCP switch is stopped, new MCP command execution is blocked even
when the token still has saved connector action permissions.

## Execution Rules

Each token/target/profile/action relationship has one rule:

- `always_run`
- `approval_required`
- `blocked`

### always_run

If the token can access the target action, the action runs directly through the
connector runtime. SSH `exec` uses the backend-owned persistent console session.

### approval_required

The connector action appears in the web UI for user approval. The MCP response
is non-blocking and returns `approval_pending` plus `request_id`.

The user can:

- Run
- Decline
- Add a note

If the user clicks Run, the gateway first verifies that the approval context is
still the one that was shown when the pending request was created. Token
permission, token validity, connector target/profile, connector metadata, or
action payload drift makes the request `stale` and requires a new AI request.

If the request is still current, the gateway executes the connector action. The
AI follows progress with `get_connector_action_request(request_id)`.

If the user clicks Decline, the request becomes `declined`; any operator note is returned as `user_note`.

### blocked

The token cannot run that connector action for that target/profile.

## Approval UI

If a connector action requires approval, it is visible in the web dashboard.

The pending command dialog should show:

- target name
- connector action
- action input
- AI reason
- token name
- request time
- Run button
- Decline button
- note field

The MCP request is not held open while waiting for the user. The AI polls with
`get_connector_action_request`.

## User Notes And Message Queue

The developer should be able to intervene without returning to the AI chat.

There are two note types:

1. approval note attached to one pending command
2. live message queue note delivered in the next matching MCP response

Example live note:

```txt
For this cluster, kubectl should be run only from control-1. Do not try kubectl on core nodes.
```

The gateway stores the message and injects it into the next matching MCP response as `user_note`.

## Database Access

Postgres uses the same connector action model:

1. Developer adds a Postgres connector target in the dashboard.
2. Gateway stores credential profiles in the encrypted vault.
3. AI receives only scoped connector actions such as `get_schemas`,
   `get_tables`, `describe_table`, and `query_readonly`.
4. Gateway validates token access and the action execution rule.
5. Query runs through the gateway.
6. Results are returned without exposing the database password.

Recommended PostgreSQL setup is a dedicated readonly database user.

Additional SQL safety hardening can grow over time:

- SELECT-only policy
- parser enforcement
- masking
- result limits
- blocked keyword checks

## Core Value

`aipermission` lets the developer say:

> "AI, you may inspect these connector targets through this token. You cannot see credentials. If approval is required, I will decide in the panel."

This reduces copy-paste terminal work while keeping the developer in control.

The goal is not to replace the developer.

The goal is to stop making the developer carry terminal output back and forth by hand.
