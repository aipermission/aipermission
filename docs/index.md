# aipermission Docs Index

This is the canonical documentation index for AIPermission. The README is the
product and quick-start entry point; this index owns detailed operator,
security, architecture, API, and contributor navigation. When two documents
overlap, prefer the document listed in its matching section below.

Start here:

- [What Is aipermission?](whatis-aipermission.md)
- [Built-In Connectors](connectors.md)
- [MVP Scope](mvp/scope.md)
- [Use Cases](mvp/use-cases.md)
- [Implementation Roadmap](mvp/implementation-roadmap.md)
- [Roadmap](ROADMAP.md)
- [Project Principles](project-principles.md)
- [Projects And Token Visibility](projects.md)
- [Project Vault](project-vault.md)
- [Support Diagnostics](support-diagnostics.md)

## Architecture

- [Local Gateway](architecture/local-gateway.md)
- [MCP Permission Flow](architecture/mcp-permission-flow.md)
- [Architecture Decisions](architecture/decisions.md)
- [Transactional Audit Outbox](adr/0007-transactional-audit-outbox.md)
- [ADR 0001: Local-Only Gateway](adr/0001-local-only.md)
- [ADR 0002: No Cloud Mode](adr/0002-no-cloud-mode.md)
- [ADR 0003: Single-User Design](adr/0003-single-user-design.md)
- [ADR 0004: SQLCipher Choice](adr/0004-sqlcipher-choice.md)

Decisions 0005 and 0006 are recorded in the consolidated
[Architecture Decisions](architecture/decisions.md). Standalone ADR files were
not published for those two decisions; numbering is intentionally preserved.

## Security

- [Credential Boundary](security/credential-boundary.md)
- [Threat Model](security/threat-model.md)
- [SSH Key Model](security/ssh-key-model.md)
- [Backup Restore](security/backup-restore.md)
- [Storage Encryption](security/storage-encryption.md)
- [Native Dependency Inventory](security/native-dependencies.md)

## Development

- [Development Architecture](development/architecture.md)
- [Add A Connector](development/add-a-connector.md)
- [Development Testing](development/testing.md)
- [Good First Issue Pool](community/good-first-issues.md)
- [GitHub Labels](maintainers/labels.md)
- [Maintainer Operations And Recovery](maintainers/operations.md)

## Setup

- [Docker Runtime](setup/docker-runtime.md)
- [Database Migration](setup/database-migration.md)
- [MCP Client Setup](setup/mcp-client-setup.md)
- [Kafka / Redpanda Connector](setup/kafka-redpanda.md)
- [Mail Connector](setup/mail.md)
- [S3-Compatible Storage](setup/s3.md)

## Providers

- [AIPermission Backup](providers/aipermission-backup.md)

## API And MCP Contracts

- [REST API](api/rest-api.md)
- [Generated OpenAPI Contract](api/openapi.json)
- [MCP Tools](api/mcp-tools.md)

## Project Skills

- [aipermission Docs Skill](skills/aipermission-docs/SKILL.md)
- [aipermission Operator Skill](skills/aipermission-operator/SKILL.md)

## Open Questions

- How can manual terminal history capture improve for interactive programs and
  shells without reliable command boundaries? Manual command history already
  exists; a terminal transcript does not prove every command's exit status.
- How strict should each connector be about read-only defaults, schema masking, and credential profile boundaries?
- How should advanced command risk analysis connect to the approval flow?
