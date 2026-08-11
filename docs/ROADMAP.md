# Roadmap

AIPermission is a local-only, single-user permission gateway for developers and
AI agents. The 0.2 line is connector-native: SSH, Postgres, ClickHouse,
Redis / Valkey, RabbitMQ, Kafka / Redpanda, S3, Docker, Kubernetes, and Mail all
use the same target, credential profile, action permission, approval, history,
audit, and project-scoping pipeline.

Shipped release details live in the [changelog](../CHANGELOG.md). This document
tracks active direction rather than repeating release history.

Related documentation:

- [What Is AIPermission?](whatis-aipermission.md)
- [Project Principles](project-principles.md)
- [MVP Scope](mvp/scope.md)
- [Local Gateway](architecture/local-gateway.md)
- [MCP Permission Flow](architecture/mcp-permission-flow.md)
- [Credential Boundary](security/credential-boundary.md)
- [Threat Model](security/threat-model.md)
- [Add A Connector](development/add-a-connector.md)

## Now

The current maintenance cycle focuses on reliability and contributor clarity:

- Keep connector behavior isolated inside connector packages and frontend
  templates while strengthening the shared permission pipeline.
- Improve graceful process shutdown, authentication backoff isolation, action
  idempotency, and audit durability.
- Split oversized frontend modules into behavior-owned components without
  changing established workflows.
- Expand behavioral tests, browser coverage, connector conformance checks, and
  typed REST documentation.
- Keep dependency, secret-scan, release, and container supply-chain checks
  visible and reproducible.

## Next

- Add directory, recursive, and glob semantics to SSH file transfer with
  explicit previews, limits, overwrite behavior, progress, and cancellation.
- Improve SSH partial output, configurable default shells, recovery hints, and
  manual terminal capture while preserving normal terminal behavior.
- Add read-first Prometheus / Grafana and GitHub / GitLab connectors with
  narrowly scoped profiles and guarded write actions.
- Extend the shared SQL toolkit through SQL Server and MySQL / MariaDB without
  hiding dialect-specific safety rules inside a universal SQL abstraction.
- Add bounded Elasticsearch / OpenSearch and MongoDB browsing before guarded
  mutations.

## Later

- Add declarative API connector recipes after their validation, credential,
  approval-preview, and action-execution boundaries are fully specified.
- Evaluate NATS / JetStream and additional database or analytics connectors
  only when there is a real dogfooding use case.
- Add optional command policy and risk-scoring helpers as warnings or deny
  rules, never as a replacement for explicit permissions and approvals.
- Consider SSH agent, ProxyJump/bastion, MFA, and SOCKS support if the local
  operator experience remains understandable.
- Consider signed backup manifests only after a separate recovery-key workflow
  is designed.

## Connector Guardrails

- Every connector uses the shared target, profile, action permission,
  approval, history, audit, and project pipeline.
- Connector-specific backend behavior and frontend UI stay in that connector's
  directory. Adding a normal connector must not require kind-specific branches
  in shared routes, permission code, history, or MCP tools.
- Permissions bind token + target + credential profile + action. UI presets are
  conveniences, not the security authority.
- Agents receive action definitions and profile references, never raw
  credential values.
- Approval-required actions snapshot the prepared request and relevant target,
  credential, permission, connector, and transport identity.
- Structured data connectors use bounded output, timeouts, read-first defaults,
  and connector-owned validation.
- Destructive queue, storage, database, and infrastructure actions remain
  explicit, high-risk, and Prompt-first.
- Code-defined connectors remain in-tree for 0.2.x. External code loading is
  out of scope.

## Project Boundaries

AIPermission intentionally remains:

- Local-only.
- Single-user.
- Developer-focused.
- Connector-based.
- Human-in-the-loop.

Hosted SaaS mode, multi-tenant architecture, remote gateway hosting, shared
team deployments, LAN-accessible gateway mode, and cloud-managed execution are
outside the project scope.

## Maintenance Rules

- Add new migrations instead of editing a migration that shipped in a public
  release.
- Keep public docs, UI copy, comments, and package metadata in English.
- Keep README, SECURITY, architecture decisions, MCP docs, and REST docs aligned
  with behavior.
- Keep local-only warnings visible in public docs and release notes.
- Keep Prompt as the recommended default for normal AI-assisted work.
- Update screenshots when the primary UI changes materially.
