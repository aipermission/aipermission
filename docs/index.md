# aipermission Docs Index

This folder is the central Obsidian-style documentation vault for aipermission.

Start here:

- [What Is aipermission?](whatis-aipermission.md)
- [MVP Scope](mvp/scope.md)
- [Use Cases](mvp/use-cases.md)
- [Implementation Roadmap](mvp/implementation-roadmap.md)
- [Roadmap](ROADMAP.md)
- [Project Principles](project-principles.md)

## Architecture

- [Local Gateway](architecture/local-gateway.md)
- [MCP Permission Flow](architecture/mcp-permission-flow.md)
- [Architecture Decisions](architecture/decisions.md)
- [ADR 0001: Local-Only Gateway](adr/0001-local-only.md)
- [ADR 0002: No Cloud Mode](adr/0002-no-cloud-mode.md)
- [ADR 0003: Single-User Design](adr/0003-single-user-design.md)
- [ADR 0004: SQLCipher Choice](adr/0004-sqlcipher-choice.md)

## Security

- [Credential Boundary](security/credential-boundary.md)
- [Threat Model](security/threat-model.md)
- [SSH Key Model](security/ssh-key-model.md)
- [Backup Restore](security/backup-restore.md)
- [Storage Encryption](security/storage-encryption.md)

## Development

- [Development Architecture](development/architecture.md)
- [Add A Connector](development/add-a-connector.md)
- [Development Testing](development/testing.md)
- [Good First Issue Pool](community/good-first-issues.md)
- [GitHub Labels](maintainers/labels.md)

## Setup

- [Docker Runtime](setup/docker-runtime.md)
- [Database Migration](setup/database-migration.md)
- [MCP Client Setup](setup/mcp-client-setup.md)

## API And MCP Contracts

- [REST API](api/rest-api.md)
- [MCP Tools](api/mcp-tools.md)

## Project Skills

- [aipermission Docs Skill](skills/aipermission-docs/SKILL.md)
- [aipermission Operator Skill](skills/aipermission-operator/SKILL.md)

## Open Questions

- How should manual terminal command event parsing be added to structured history?
- How strict should each connector be about read-only defaults, schema masking, and credential profile boundaries?
- How should advanced command risk analysis connect to the approval flow?
