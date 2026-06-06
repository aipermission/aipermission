# Roadmap

AIPermission has published its first public release candidate. The local-only
SSH/MCP MVP is usable today; the next releases focus on dogfooding polish,
small safety improvements, and clearer contributor paths.

Related notes:

- [What Is AIPermission?](whatis-aipermission.md)
- [MVP Scope](mvp/scope.md)
- [Use Cases](mvp/use-cases.md)
- [Local Gateway](architecture/local-gateway.md)
- [MCP Permission Flow](architecture/mcp-permission-flow.md)
- [Credential Boundary](security/credential-boundary.md)
- [Threat Model](security/threat-model.md)
- [Project Principles](project-principles.md)

## Current Status

`0.1.0-rc.1` shipped the first local developer workflow:

- Docker Compose local runtime at `http://localhost:3210`.
- SQLCipher encrypted named `.aipdb` databases.
- Local UI session and CSRF protection for web REST mutations.
- Gateway-owned and explicitly imported SSH keys, SSH host fingerprint approval,
  SSH host import from OpenSSH config files, and `known_hosts`
  pinning.
- Server inventory, server hints, SSH key install commands, and uninstall
  cleanup.
- MCP tokens with `always_run`, `approval_required`, and `blocked` execution
  rules.
- Token expiration, reusable-token opt-in, and global MCP Started/Stopped
  switch.
- Persistent console sessions with live attach, AI command display, messages,
  transcript chunks, and history.
- Queued SSH/SFTP upload and download from the local web UI, with separate File
  Transfer History.
- Approval dialogs, user notes, AI-to-user messages, audit logs, and searchable
  History/Audit pages.
- Redaction settings with built-in and custom regex rules.
- MCP npm package, setup CLI, operator skill installer, and public package
  metadata.
- README screenshots, ADRs, security policy, contributing guide, release
  checklist, CodeQL, Dependabot, tests, audits, and secret scan.

## Project Boundaries

AIPermission is intentionally:

- Local-only.
- Single-user.
- Developer-focused.
- SSH-based.
- Human-in-the-loop.

These requests conflict with the project principles and should normally be
closed as `wontfix`:

- Hosted SaaS mode.
- Multi-tenant architecture.
- Remote gateway hosting.
- Shared team deployments.
- LAN-accessible gateway mode.
- Cloud-managed command execution.

## Shipped Follow-Up Polish

Small follow-up releases focus on daily-use friction found while using
AIPermission against real VPS maintenance tasks.

`0.1.1` shipped:

- Collapsible Console side panels for smaller screens.
- Meaningful Console status dots for no live session, idle, and busy states.
- Browser title that shows MCP Started/Stopped and the active database.
- Safer database deletion with a second password confirmation.
- Manual update checks from the in-app Changelog dialog.
- Bulk token permission updates across all servers.
- Approval-run notes delivered back to the AI.

`0.1.2` ships:

- History labels for tagging command requests and filtering History by label.
- History label cleanup from Settings without deleting command history records.
- On-demand Docker quick checks from the Servers page.
- Docker container details and tail-configurable Docker logs dialogs.

`0.1.4` ships:

- Manual Console command logging for simple terminal input.
- Best-effort manual command output capture for simple typed or pasted commands.
- History source filters and badges for MCP and manual command records.
- MCP request APIs explicitly scoped to MCP-origin command requests.
- No shell hooks or hidden command suffixes; normal Console terminal behavior is unchanged.
- Interactive commands, nested shells, heredocs, and unsafe control sequences
  remain untracked best-effort rows. Arrow/history recall uses a placeholder
  command because the terminal does not send the recalled command text; simple
  recalled commands may still capture output when the prompt returns.

`0.1.5` ships:

- Single-file upload to a selected SSH server over SFTP.
- Single-file remote download over SFTP, served through the browser after the
  transfer reaches `completed`.
- File Transfer History with pagination, search, server/status/direction
  filters, status/progress metadata, checksums, and detail view.
- File contents are not stored in SQLCipher; only metadata and progress are
  persisted.

`0.1.6` ships:

- Queued upload and download flows from Console.
- Multi-file upload with ordering, removal, overwrite confirmation, progress,
  speed, and ETA.
- Multi-file remote download with final zip packaging when more than one file is
  selected.
- Process-local pause/resume for active transfer queues, plus cancel behavior.
- Batch transfer REST endpoints for queue status, pause, resume, cancel, and
  final download delivery.

`0.1.7` ships:

- MCP transfer metadata, remote browse, remote download queue start, direct
  local save, direct local upload, and pause/resume/cancel tools.
- Transfer Center in the sidebar for monitoring active and recent UI/MCP
  transfer queues.
- MCP transfer responses stay metadata-only and never include file contents or
  gateway temporary paths.

## Early RC Follow-Ups

These are good candidates for small follow-up releases after the first public
tag:

- [ ] Add permission expiration for temporary server access grants.
- [ ] Add temporary `always_run` grants with visible countdowns in Console.
- [ ] Add command policy/risk scoring primitives without turning the product
  into a DevOps platform.
- [ ] Add optional deny/warn rules for common high-risk command patterns.
- [ ] Add stronger manual command capture for shell history recall, arrow keys,
  and cursor-edited commands. Do this with a deliberate frontend submitted-line
  signal or shell-assisted marker model instead of backend escape-sequence
  guessing, and preserve normal terminal behavior as the first invariant.
- [ ] Add directory transfer, recursive copy, remote glob handling, and
  restart-surviving resumable transfer design after bulk transfer semantics are
  dogfooded.
- [ ] Add `approval_required` flow for file transfers after command approvals
  and Transfer Center semantics have been dogfooded together.
- [ ] Add optional safety backup before import.
- [ ] Add more Playwright browser tests for Settings, import, token permission,
  and approval workflows.
- [ ] Expand frontend component tests.
- [ ] Add more MCP client setup examples as the client ecosystem changes.
- [ ] Add a documented release cadence for small RC follow-up updates.
- [ ] Open and maintain a visible good-first-issue pool.

## Later Ideas

These may be useful, but they are not required for the RC:

- [ ] SQL query tools with explicit database credential boundaries.
- [ ] Optional sensitive/no-persist console sessions.
- [ ] Export filters for History and Audit data.
- [ ] FTS5 search only if the SQLCipher driver/build supports it without
  increasing install friction. The current release uses SQLCipher-compatible
  FTS4 search.
- [ ] Chunk retention and compaction controls for very long console sessions.

## Maintenance Rules

- After the first public tag, do not edit migration `1`; add migration `2+` for
  schema changes.
- Keep docs, UI copy, comments, and package metadata in English.
- Keep README, SECURITY, ADRs, MCP docs, and REST docs aligned with the code.
- Keep local-only warnings visible in public docs and release notes.
- Keep `approval_required` as the recommended default for normal AI-assisted
  work.
- Keep screenshots current when the main UI changes.
