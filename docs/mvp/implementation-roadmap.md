# Implementation Roadmap

This file gives the high-level sprint sequence. The detailed checklist lives in [Roadmap](../ROADMAP.md).

## Sprint 1: Runnable Skeleton

- Docker Compose
- Go backend
- React frontend
- SQLite volume
- `GET /health`
- frontend status view

Status: implemented.

## Sprint 2: Gateway Data Model

- versioned migrations
- connector target table
- SSH key table
- token table
- token action permission table
- command request table
- message queue table
- audit table
- settings table
- vault abstraction

Status: implemented. `command_requests` powers MCP execution, approval flow, and History UI. `message_queue` powers user-to-AI and AI-to-user notes. `audit_logs` stores important token, permission, settings, console, MCP, and approval events.

## Sprint 3: Server, Token, Permission UI

- Credentials page
- `ed25519` and `rsa` keypair generation
- public key install command
- server CRUD
- token create/revoke
- reusable token copy setting
- token permission dot summary
- backend-owned persistent PTY console sessions
- console-side token action permission editing
- execution rule selection: blocked, prompt, always run

Status: implemented.

## Sprint 4: SSH Execution

- gateway-generated SSH key authentication
- SSH credential profile selection on connector targets
- command timeout
- stdout/stderr/exit code capture
- command result persistence
- first-connect host fingerprint approval and known_hosts verification

Status: implemented.

## Sprint 5: MCP Core

- npm MCP package
- `init` CLI for provider config
- `install-skill` / operator instruction installer
- `list_connector_targets`
- `get_connector_help`
- `get_connector_actions`
- `call_connector_action`
- `get_connector_action_request`
- token-scoped access
- direct `always_run` execution

Status: implemented.

## Sprint 6: Approval Flow

- pending command creation
- non-blocking `approval_pending` plus `request_id`
- approval polling
- Run
- Decline
- approval note
- console badges and dialog

Status: implemented.

## Sprint 7: Message Queue And Audit

- Console message drawer
- send-once user-to-AI messages
- AI-to-user messages
- unread badges
- `user_note` injection
- audit events for security-relevant actions

Status: implemented. A dedicated audit browsing UI can still be improved.

## Sprint 8: Setup Documentation And Demo

- MCP setup page
- `.env.example`
- README simplification
- local demo scenario
- token revoke validation
- public release hardening

Status: in progress.
