# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses semantic versioning once public releases begin.

## [Unreleased]

## [0.1.11] - 2026-06-08

### Added

- Servers can now store optional advanced SSH startup settings for NAS and
  appliance targets that show an interactive menu before a normal shell.
- Advanced startup settings support post-connect input, such as `q\n` for some
  QNAP menus, and an optional forced shell command for compatibility targets.

### Changed

- Updated the Go toolchain/Docker builder image, frontend dependencies, and the
  SFTP dependency after local test verification.

### Fixed

- Windows checkouts now preserve LF line endings for Docker shell scripts, with
  a hygiene check to catch CRLF entrypoint regressions.
- Console recovery banners now distinguish manual console commands from MCP/AI
  commands.
- SSH command execution, Docker checks, and connection tests now share clearer
  timeout, connection refused, authentication, and host-key error messages.
- Basic redaction no longer masks normal shell `PWD=/path` output while still
  masking lowercase `pwd=...`, password, token, API key, bearer token, and
  private-key patterns.
- README and operator instructions now clarify that `list_servers` is
  permission-scoped and not a live SSH health check.

## [0.1.10] - 2026-06-08

### Added

- Console now shows active long-running MCP commands for the selected server,
  including running age, token label, command, reason, and stuck-session
  guidance.
- Local operators can restart a server-scoped persistent console session from
  the Console UI when it appears wedged.
- MCP running-request hints and operator instructions now document the recovery
  sequence: poll `get_request`, inspect `read_console`, then use
  `restart_console_session(server_id)` when no useful progress is visible.

### Fixed

- Hide internal persistent-console prelude lines from the live console and MCP
  command output when a PTY echoes setup commands.
- MCP server-list hints now clarify that `list_servers` is permission-scoped;
  agents should rely on `exec` dial, timeout, SSH authentication, and host-key
  errors for current reachability.

## [0.1.9] - 2026-06-07

### Added

- Pending MCP command approvals now store an approval-context snapshot covering
  the token, token/server permission, server profile, SSH key fingerprint, MCP
  tool metadata, and command payload hash.
- Approval dialogs show how long ago the request was created.
- MCP clients can restart a stuck persistent console session for a visible
  server, causing the next `exec` call to open a fresh SSH session.

### Security

- If a pending command's approval context changes before the operator clicks
  Run, AIPermission marks the request `stale` and requires the AI to submit a
  fresh request instead of executing an old approval.

### Fixed

- MCP command execution is more resilient when a persistent console session is
  closed or restarted while a command request is still running.

## [0.1.8] - 2026-06-07

### Added

- Token/server permissions can now have an optional `expires_at` timestamp for
  temporary maintenance grants.
- Console token controls can turn active Prompt or Always permissions into
  1-hour, 4-hour, or 1-day temporary grants.
- The Console always-run warning shows a countdown when an active `always_run`
  grant is temporary.
- MCP `list_servers` includes `expires_at` for temporary grants and omits
  expired grants.

### Security

- Expired token/server permissions are not treated as effective by MCP command,
  console, file-transfer, or server-list permission checks.
- Permission expiration is a local safety rail for temporary maintenance
  windows. It does not change the local-only threat model or make exposed
  gateway ports safe.

## [0.1.7] - 2026-06-06

### Added

- MCP file transfer status tools for token-scoped transfer and batch metadata.
- MCP remote directory browsing and remote download queue start tools for
  `always_run` server permissions.
- MCP `save_file_download` for writing completed gateway downloads to an
  explicit local path from the local MCP process.
- MCP `upload_files` for uploading explicitly named local files to a remote
  directory through the gateway.
- MCP transfer start tools now support `approval_required` server permissions by
  creating a local Transfer Center approval queue where selected files can be
  approved and the rest rejected with a note.
- MCP pause, resume, and cancel tools for active transfer queues.
- Transfer Center in the local UI for monitoring active and recent UI/MCP
  transfer queues without keeping the original dialog open.

### Security

- MCP transfer responses never include local temporary paths, local upload
  paths, archive staging paths, or file contents.
- MCP direct transfer tools require explicit local paths. `always_run` starts
  queues immediately, while `approval_required` creates a local Transfer Center
  approval queue before touching the remote server.

## [0.1.6] - 2026-06-06

### Added

- Queued SSH/SFTP uploads and downloads from the local Console UI.
- Multi-file upload queue with per-file ordering, removal, overwrite
  confirmation, live progress, speed, and ETA.
- Multi-file remote download queue with zip packaging after remote downloads
  complete.
- Pause and resume controls for active transfer queues while the gateway process
  remains running.
- Batch transfer REST endpoints for queue status, pause, resume, cancel, and
  final download delivery.
- Duplicate remote paths are rejected before transfer start; download zip
  entries get stable numeric suffixes when remote basenames collide.

### Security

- File contents remain outside SQLCipher. Transfer history stores metadata,
  status, progress, checksum, path, and errors only.
- Uploads are written to a temporary remote file first and moved into place only
  after the upload completes, so canceling an upload does not leave a partial
  target file behind.
- Download batches are capped at 1 GiB total remote file size.
- Pause/resume is intentionally process-local. If the gateway process, Docker
  container, or computer restarts, unfinished transfer queues should be started
  again instead of resumed from old local state.
- MCP file-transfer tools remain intentionally unavailable while UI transfer
  safety and audit semantics are dogfooded.

## [0.1.5] - 2026-06-05

### Added

- SSH file transfer history model for upload/download metadata, status,
  progress, checksums, and errors.
- UI-driven single-file upload over SFTP from Console.
- UI-driven single-file remote download over SFTP, with browser download after
  completion.
- Remote file/folder browser for selecting SFTP upload directories and download
  files from the local UI.
- Cancel support for pending or running UI file transfers.
- Explicit overwrite confirmation before replacing an existing remote file.
- File Transfer History tab with pagination, search, server/status/direction
  filters, live progress display, and detail dialog.

### Security

- File contents are never stored in SQLCipher. Uploads are staged in a private
  temporary directory and removed after the remote transfer finishes or fails.
- Remote downloads are staged in a private temporary file and served through the
  browser only after the transfer reaches `completed`; temporary downloads are
  short-lived.
- File transfer is currently exposed through the local web UI only. MCP
  file-transfer tools are intentionally not exposed in this release.

### Notes

- This is a conservative single-file MVP. Directory transfer, recursive copy,
  remote glob expansion, resumable transfers, and SSH-agent/ProxyJump based
  transfer transports are still future work.

## [0.1.4] - 2026-06-05

### Added

- Manual Console command logging for simple terminal input. Manually typed or
  pasted commands are recorded as `source = manual` without changing normal
  terminal behavior.
- Best-effort output capture for simple manual commands. When the shell prompt
  returns, History records captured output and marks the command `completed` or
  `canceled`.
- History source filters and badges for MCP and manual command records.
- `source`, `tracking_reason`, and `output_truncated` command request fields for
  manual command records.

### Security

- MCP request list/detail APIs explicitly remain scoped to MCP-origin command requests so manual History rows cannot leak through MCP tools.

### Notes

- Interactive commands, nested shells, heredocs, and unsafe control sequences are
  stored as `untracked` best-effort records. Arrow/history recall is stored with
  a placeholder command because the terminal does not send the recalled command
  text; simple recalled commands may still capture output when the prompt
  returns, while ambiguous interactive recalled commands are left `untracked`.
- This release does not install shell hooks, append hidden command suffixes, or
  infer shell history recall from arrow keys.

## [0.1.3] - 2026-06-04

### Added

- Existing SSH private key import with optional import-time passphrase handling.
- SSH host import from OpenSSH config files and pasted config content for prefilling server records.

### Changed

- Terminal-like command, output, log, and setup code blocks now share consistent typography.
- SSH host import avoids sending `IdentityFile` paths into server descriptions and reports `ProxyCommand` without returning the raw command.
- SSH config parsing follows OpenSSH-style first-value-wins behavior for matching `Host` blocks.
- Imported RSA private keys must be at least 2048 bits.

## [0.1.2] - 2026-06-03

### Added

- History labels for tagging command requests, filtering history by label, and cleaning up labels from Settings without deleting history records.
- On-demand Docker quick checks from the Servers page for current container status and exposed ports.

## [0.1.1] - 2026-06-02

### Added

- Manual GitHub release update check in the in-app changelog dialog.
- Bulk token permission updates for applying one rule to every server.
- Optional approval-run notes that are delivered back to the AI after approval.

### Changed

- Console side panels can collapse for narrower screens.
- Browser title now shows the MCP runtime state and active database name after unlock.
- Console server status dots now reflect live session, pending, and running state instead of decorative window controls.
- Database deletion now requires a second confirmation dialog with the current database password.

### Notes

- This release is focused on dogfooding polish after the first public RC.

## [0.1.0-rc.1] - 2026-06-02

### Added

- Local-only Docker gateway with React UI on `http://localhost:3210`.
- SQLCipher-encrypted named `.aipdb` databases with unlock, switch, import, backup, rename, delete, and password-change flows.
- Gateway-owned SSH key generation, public-key install commands, SSH host fingerprint approval, and server records.
- Token-scoped MCP command execution with `always_run`, `approval_required`, and `blocked` rules.
- Persistent web console sessions, live output streaming, approval dialogs, messages, command history, and audit logs.
- `@aipermission/mcp` package with init and operator-skill installer for common AI clients.
- Security controls for local-only HTTP boundaries, UI session cookies, CSRF, redaction rules, reusable-token opt-in, and supply-chain checks.

### Security

- SSH private keys and reusable token values stay inside the local encrypted gateway.
- API tokens are stored as hashes and shown once by default.
- Approval-required raw commands are encrypted separately so display redaction cannot mutate execution payloads.
- `read_console` requires `always_run` permission to avoid exposing unrelated manual transcripts to approval-only tokens.

### Notes

- This RC is a local developer gateway, not a remote DevOps platform.
- Do not expose the UI/API on a LAN or the public internet.
