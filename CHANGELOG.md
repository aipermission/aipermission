# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses semantic versioning once public releases begin.

## [Unreleased]

### Added

- History labels for tagging command requests, filtering history by label, and cleaning up labels from Settings without deleting history records.

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
