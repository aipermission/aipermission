# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses semantic versioning once public releases begin.

## [Unreleased]

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
