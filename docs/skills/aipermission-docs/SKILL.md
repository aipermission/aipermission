---
name: aipermission-docs
description: Use when creating, updating, reorganizing, or reviewing aipermission documentation. Maintains project-root summary docs and the central docs tree under docs/, using GitHub Markdown relative links, small linked notes, local Docker/MCP/API-token/approval/vault/security consistency, and AI-friendly context boundaries.
---

# aipermission Docs

## Core Rule

Treat aipermission documentation as part of the product.

All repository documentation, comments, UI text, examples, skills, and public-facing strings must be written in English unless a future task explicitly requires a localized translation artifact. Do not add Turkish text to docs, code comments, tests, or screenshots.

When documenting a change, keep the docs useful for two readers:

- humans operating or building the local gateway
- future AI agents that should load only the context they need

Prefer small linked notes over long architecture essays.

## Link Rule

The documentation root is:

```text
docs/
```

Write links as GitHub Markdown relative links from the current file:

```markdown
[Local Gateway](../../architecture/local-gateway.md)
[MCP Permission Flow](../../architecture/mcp-permission-flow.md)
[Credential Boundary](../../security/credential-boundary.md)
[MCP Client Setup](../../setup/mcp-client-setup.md)
[MVP Scope](../../mvp/scope.md)
```

From nested docs, use `../` as needed:

```markdown
[MCP Tools](../../api/mcp-tools.md)
```

Do not use Obsidian-style wikilinks in public docs. GitHub readers must be able to click links directly.

## Documentation Layers

### Project Root Docs

Keep root docs short and practical:

```text
README.md
docs/ROADMAP.md
env.example
docker-compose.yml
```

Root docs should explain:

- what aipermission is
- how to run it
- local ports
- required environment variables
- links to central docs

Do not put long architecture essays in root docs. Link to central docs for deep context.

### Central Docs

Central docs live under `docs/`:

```text
docs/
  index.md
  architecture/
  adr/
  security/
  setup/
  mvp/
  api/
  community/
  maintainers/
  skills/
```

Use focused notes:

- `architecture/` for gateway, MCP, approval, data flow
- `adr/` for stable architecture and product-scope decisions
- `security/` for credential, token, audit, threat boundaries
- `setup/` for Docker and MCP client setup
- `mvp/` for scope, roadmap, demo flows
- `api/` for REST/MCP contracts when they stabilize
- `community/` for contributor-friendly issue pools
- `maintainers/` for label, triage, and release-maintenance notes
- `skills/` for project-local Codex skill definitions

## aipermission Consistency Checks

When updating docs, check whether the change touches:

- local Docker runtime
- backend port, frontend port, MCP endpoint
- SQLite data volume
- encrypted vault behavior
- gateway secret / master password assumptions
- SSH credential storage
- API token creation, masked UI display, copy behavior, revoke behavior
- token-to-server permissions
- execution rules: `always_run`, `approval_required`, `blocked`
- MCP tools: `list_servers`, `exec`
- approval dashboard behavior
- Run / Decline / approval note behavior
- live message queue behavior
- audit log contents
- backup download/import behavior
- PostgreSQL/query roadmap
- security boundary: credentials never leave gateway
- developer-tool positioning vs DevOps-platform positioning
- local-only gateway positioning vs remote-hosted/LAN-shared positioning
- project principles and `wontfix` boundaries
- ADR links when product-scope decisions are affected

If yes, update the related central docs and local summary docs together.

## Naming And Product Boundaries

Use the established aipermission naming:

- `aipermission` is the product name.
- `gateway` is the local backend that owns credentials, policy, execution, approvals, and audit.
- `MCP client` means Cursor, Windsurf, or another AI tool integration.
- `API token` means the gateway access token used by MCP/API clients.
- `server` means a remote SSH target stored in the gateway.
- `database` means a configured DB target, initially PostgreSQL in the roadmap.
- `execution rule` means `always_run`, `approval_required`, or `blocked`.
- `approval note` means a note attached to one pending command.
- `live message queue` means a user message delivered through the next MCP response.

Do not describe the MVP as a full DevOps platform, orchestration platform, or permanent production control plane.

Do not describe the gateway as remotely hostable, LAN-shareable, or suitable for central team access. Remote machines are SSH targets; the gateway itself stays on the developer's local machine.

## Security Documentation Rules

Always preserve these rules:

- SSH passwords and private keys never leave the gateway.
- Prefer the Dokploy-style SSH key model: gateway generates SSH keypairs and users paste the public key install command on their VPS.
- Do not document SSH password collection as the preferred MVP path.
- Database credentials never leave the gateway.
- MCP responses never include credentials.
- API tokens are not SSH or DB credentials.
- API tokens are masked in the UI and can be copied again for local MCP setup.
- Token storage is protected by the local SQLCipher database password.
- Revoked tokens must stop working immediately.
- Backup files are raw SQLCipher `.aipdb` database downloads protected by the database password.
- The encrypted DB must include enough vault material to restore encrypted SSH keys on another machine.
- `list_servers` returns only servers allowed for that token.
- `exec` checks token validity, target permission, and execution rule before running.
- Audit logs must not contain credential values.

When a doc mentions credential storage, also mention SQLCipher database encryption, unrecoverable database password behavior, and the gateway vault encryption layer for secret payloads.

## MCP Documentation Rules

Document MCP behavior through explicit tool contracts.

For MVP, keep the public MCP surface to:

```text
list_servers()
exec(server_id, command, reason?)
exec(server_ids, command, reason)
read_console(server_id, tail?)
restart_console_session(server_id)
get_request(request_id)
list_requests(status?)
send_message(message, server_id?, session_id?)
```

When writing examples, show:

- token-scoped server visibility
- `always_run` response
- `approval_required` non-blocking `approval_pending` + `request_id` behavior
- `get_request` polling behavior after approval
- `send_message` AI-to-user behavior
- Console Messages user-to-AI `user_note` consumption
- `declined` response with `user_note`
- `blocked` response

Do not imply the AI can open arbitrary SSH sessions or see credentials.

## Approval And Message Queue Rules

When documenting command execution, include the approval decision path:

```text
MCP exec request
-> token validation
-> target permission check
-> execution rule check
-> direct run or pending approval
-> approval_pending + request_id for prompt flow
-> Run / Decline
-> get_request result / decline / blocked response
```

For notes, separate:

- approval note: attached to a specific pending command
- live message queue: delivered in the next MCP response

For MVP, describe live messages as `send once`. User-to-AI notes may be generic or server/session scoped; server-scoped notes must only be consumed by MCP responses for that same server. Sticky messages are future work.

## Docker Setup Documentation Rules

When documenting local setup, include:

- `docker compose up`
- frontend URL
- backend URL
- MCP/API URL
- SQLite volume or data path
- gateway secret environment variable
- `.env.example` when environment variables exist

The default product posture is local-only. Do not describe remote gateway
hosting, LAN sharing, hosted SaaS mode, or team deployments as future work.
Those requests conflict with the project principles and should be documented as
out of scope unless the project intentionally creates a separate fork/product.

## PostgreSQL Documentation Rules

PostgreSQL is part of the near roadmap, not the first server-only slice unless implemented.

When documenting it, keep the boundary clear:

- AI never sees DB passwords.
- Gateway stores DB credentials in the vault.
- Recommended DB user is readonly.
- Future tools can be `list_databases()` and `query(database_id, sql, reason?)`; do not document them as implemented until the MCP/API surface exists.
- Advanced SQL safety such as parser enforcement, masking, limits, and readonly transactions can be deferred.

## Index Maintenance

When adding an important central doc, update:

```text
docs/index.md
```

Add the note under the right section with a GitHub Markdown relative link.

## Documentation Update Flow

When asked to document aipermission:

1. Read existing docs first.
2. Identify whether the change affects product positioning, setup, architecture, security, MCP/API contracts, MVP scope, or roadmap.
3. Update concise root docs only when directly affected.
4. Update or create focused central docs under `docs/`.
5. Update `docs/index.md` for important central docs.
6. Update `docs/ROADMAP.md` when roadmap or implementation order changes.
7. Keep GitHub Markdown relative links.
8. Mark unknowns as open questions instead of guessing.
9. Keep security boundaries explicit.

## Quality Bar

Good aipermission docs are:

- accurate to the current implementation or clearly marked as planned
- split into small linked notes
- easy to navigate from `docs/index.md`
- explicit about local Docker runtime
- explicit about MCP and API token flow
- explicit about credential boundaries
- explicit about approval behavior
- light enough for future AI context loading

## Final Response

When reporting documentation work, include:

- root docs updated
- central docs updated
- important links added
- open questions
- areas intentionally left unchanged
