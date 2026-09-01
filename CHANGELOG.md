# Changelog

<!-- Generated from release-notes.json. Do not edit directly. -->

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses semantic versioning for public releases.

## [Unreleased]

## [0.2.38] - 2026-09-01

### Changed

- Frontend connector forms now share one tested network transport field implementation
  without changing connector-owned behavior.
- JavaScript workspaces now use package-local lockfiles as the canonical dependency
  boundary for reproducible frontend and MCP builds.

### Security

- The local maintenance console now runs with an isolated environment and supervised
  process lifecycle so database credentials, proxy settings, and stale child processes
  do not escape their intended boundary.
- Container releases now build immutable source candidates, verify the exact source
  commit, and promote only validated image digests to public version and latest tags.
- MCP configuration and installed skill files now use private, atomic file handling with
  package-level lint, format, test, build, and audit gates.
- The frontend runtime image now upgrades Expat to the patched Alpine package that
  closes CVE-2026-66046 and CVE-2026-76641.

### Maintenance

- Frontend risk coverage now exercises approval dialogs, transfers, unlock and restore
  flows, maintenance console lifecycle, and connector transport forms across
  asynchronous failure paths.
- Release automation now verifies pinned release images, package lock ownership, MCP
  package quality, and container publication workflow behavior before promotion.

## [0.2.37] - 2026-09-01

### Fixed

- Docker connector execution now accepts only the standard CLI or an absolute wrapper,
  verifies Docker identity, and shares the same renderer across actions and live
  consoles.
- S3 object rename is disabled because S3-compatible APIs cannot provide an atomic
  cross-key move; source deletion remains a separate destructive operator decision.

### Security

- New direct remote Postgres targets now default to hostname-verified TLS while explicit
  and legacy transport modes remain compatible.
- Backup service requests ignore ambient process proxies and refuse redirects so
  encrypted snapshots and provider tokens stay on the operator-selected route.
- Database mutation paths now preserve affected-row driver failures instead of treating
  them as successful zero-row updates.
- The backend now uses the patched Go cryptography module release that closes
  CVE-2026-56854 in SSH source-address restriction enforcement.

## [0.2.36] - 2026-08-31

### Changed

- Connector target and credential profile lifecycle code now shares one frontend
  implementation across built-in connector templates.
- Dense generic connector action, connector target, and file transfer modules now use
  smaller behavior-owned boundaries without changing connector contracts.
- The product overview is shorter and canonical connector documentation is generated
  from connector metadata to reduce semantic drift.

### Fixed

- Managed credential deletion now requires confirmed external cleanup before removing
  the local profile, and Postgres cleanup ownership reassignment is explained before
  deletion.
- File transfers now classify timeout, interruption, validation, local persistence, and
  uncertain remote outcomes while warning users to inspect the destination before
  retrying.
- Connector permission, lifetime, and action failures now remain visible and retryable
  in the interface, including non-standard JavaScript rejection values.

### Security

- Uncertain external side effects no longer encourage blind retries, and managed
  credential cleanup results are redacted before entering audit records.

### Maintenance

- Critical backend and frontend lifecycle coverage expanded with higher enforced floors
  and focused permission, cleanup, transfer, and connector architecture tests.
- Maintainer recovery guidance now records the current single-maintainer risk, emergency
  release procedure, and explicit backup ownership criteria without overstating project
  capacity.

## [0.2.35] - 2026-08-31

### Fixed

- Connector credential provisioning now compensates completed external steps when later
  secret encryption or profile persistence fails, preventing orphaned managed
  credentials.
- File transfers now preserve the difference between user cancellation and deadline
  expiry across item, batch, history, and API lifecycle updates.
- Pending connector approvals now surface activity refresh failures instead of silently
  leaving the interface with stale completion state.

### Security

- Legacy database migration now fails closed when schema probes cannot be completed,
  rather than treating probe failures as absent legacy tables.

### Maintenance

- Updated LZ4 compression support, frontend icons and test tooling, ESLint, and pinned
  CodeQL actions through maintainer-authored dependency commits.

## [0.2.34] - 2026-08-31

### Changed

- Console connection lifecycle, Redis browser state, migration maintenance, token
  dialogs, and genuinely shared credential profile fields now live in smaller
  behavior-owned modules.
- Contributor architecture and approval-flow diagrams now document the generic connector
  pipeline, while canonical recovery guidance explains safe retry and failure handling.

### Fixed

- Malformed console WebSocket frames now produce a bounded warning without terminating
  an otherwise healthy session.
- Token revocation keeps explicit success feedback visible, generated token values clear
  at lifecycle boundaries, and Redis confirmations cannot carry across target changes.
- Legacy database migration output is staged and published without overwriting an
  existing destination, with interrupted attempts cleaned up safely.

### Security

- Non-interactive MCP initialization now requires an explicit provider and protects
  tracked project configuration in normal repositories, linked worktrees, and
  submodules.
- The security policy now documents supported versions, bounded approval decryption,
  single-user deployment expectations, and clearer private vulnerability reporting.

### Maintenance

- Offline Markdown link validation and Go and frontend function-complexity budgets now
  run in repository hygiene checks.
- Deterministic recovery drills now cover the self-hosted backup download path and
  interrupted legacy migration publication.

## [0.2.33] - 2026-08-31

### Maintenance

- Critical backend packages and frontend authorization/session surfaces now have
  explicit coverage floors enforced by CI and release checks.
- A real encrypted backend browser flow now verifies Prompt approval, connector
  completion, stale-context rejection, lock/unlock, and backend restart recovery.
- Deterministic recovery drills now exercise encrypted backup/import, wrong-password
  rejection, legacy migration fixtures, and gateway-secret continuity across restart.
- Bounded fuzz gates now protect approval contexts, SQL safety, redaction, Redis
  parsing, transfer paths, backup metadata, and connector payload normalization.

### Security

- Read-only SQL validation now rejects unterminated quoted values, identifiers, dollar
  quotes, and block comments instead of passing malformed input to a connector.
- Connector number fields now reject NaN and infinite values before canonical approval
  hashing or JSON persistence.

## [0.2.32] - 2026-08-30

### Changed

- Pending connector approvals now use small typed orchestration steps while preserving
  authorization, stale-context, claim, audit, execution, and completion ordering.
- Runtime-backed connector adapters are constructed explicitly per gateway catalog
  instead of relying on package-global registration or package initialization side
  effects.

### Maintenance

- Focused transition tests now lock approval-pending, running, completion, failure,
  history, and audit ordering.
- Catalog-derived import and runtime isolation checks now cover every built-in connector
  and prevent shared gateway packages from depending on connector implementations.

## [0.2.31] - 2026-08-30

### Changed

- New ClickHouse, Redis / Valkey, and RabbitMQ targets prefer verified TLS for remote
  Direct endpoints while preserving explicit local, Over SSH, and existing saved
  transport choices.

### Fixed

- Redis, RabbitMQ, and ClickHouse now distinguish optional missing secrets from Vault or
  secret-provider failures instead of silently downgrading failed credential resolution.
- Kubernetes normal actions and live pod consoles now share one connector-owned kubectl
  command validator that rejects shell syntax before execution.

### Security

- Every database-password verification route now shares one metadata-free rate-limit
  scope, preventing endpoint rotation from bypassing unlock throttling.
- Redis string values and RabbitMQ message payloads and properties are redacted from
  persisted approval previews, history, audit, and MCP projections while exact encrypted
  execution payloads remain available to the action runner.

## [0.2.30] - 2026-08-30

### Changed

- Container and MCP publication now verify the exact tagged source commit, release
  metadata, mandatory CI and CodeQL results, package tests, builds, and pack output
  before publishing.
- Real-service connector conformance and native dependency freshness remain visible
  advisory release signals instead of unreliable unconditional external-service gates.

### Fixed

- Direct API server construction now returns workspace and runtime identity
  initialization failures instead of starting with incomplete execution identity.

### Security

- Malformed non-empty token, connector-permission, and project-capability expiry values
  now fail closed while an explicit empty expiry remains permanent.
- The local HTTP boundary now rejects missing or malformed Host and RemoteAddr values
  and accepts only parsed loopback request origins.

## [0.2.29] - 2026-08-26

### Changed

- REST contract generation now combines core routes with connector-owned adapter routes,
  keeping runtime registration, OpenAPI, and documentation tests on one inventory.
- Release content now lives in one structured source that generates the complete
  changelog and the bounded in-app release artifact.
- Updated go-smtp to 0.25.0, lucide-react to 1.33.0, testing-library user-event to
  14.6.5, and the pinned CodeQL action to 4.37.8.

### Fixed

- Release checks now detect and update MCP workspace version drift in the root lockfile.
- Generated release artifacts fail validation when stale, out of order, duplicated, or
  inconsistent with the release manifest.

### Security

- The Mail SMTP client update keeps dynamic authentication responses out of format
  strings and adds SMTPUTF8 address handling.
- The frontend runtime image upgrades Alpine OpenSSL packages to the patched release
  detected by the container security gate.
- SQLCipher 4.18.0 was reviewed with no published GitHub security advisories; the pinned
  wrapper and active SQLCipher 4.16.0 runtime remain unchanged.

## [0.2.28] - 2026-08-18

### Changed

- Updated the Go toolchain and container build baseline to 1.26.6, Franz-Go to 1.21.6,
  frontend lint globals to 17.11.0, and the pinned CodeQL action to 4.37.7.

### Fixed

- Encrypted databases now repair recorded-but-incomplete audit recovery schema changes,
  including missing outbox columns, indexes, and command lifecycle triggers.
- SSH connection establishment and command execution now use separate deadlines, so a
  slow command no longer inherits the connector dial timeout.
- Long-running commands in Vault-authorized sessions now revalidate with a fresh bounded
  context after dispatch, preserving valid leases and returning an explicit unknown
  outcome only when the result truly cannot be projected.

### Security

- Go 1.26.6 resolves the standard-library vulnerabilities detected by release
  `govulncheck` and container scanning in URL, TLS, HTTP, XML, ASN.1, and template
  processing paths.

## [0.2.27] - 2026-08-12

### Changed

- Generated MCP runtime configurations now pin the exact package version that created
  them, while explicit install and upgrade commands remain unpinned.

### Fixed

- Connector results that cannot be safely projected after remote execution now finish as
  `outcome_unknown` instead of inviting an unsafe automatic retry.

### Security

- Every connector action result now crosses one canonical JSON boundary before
  persistence or MCP projection, with global byte, depth, node, string, and key limits
  plus recursive redaction for typed and custom-marshaled values.
- Persisted connector output and the matching MCP response now reuse the same canonical
  redacted projection, preventing representation drift or typed-value redaction
  bypasses.

## [0.2.26] - 2026-08-12

### Added

- Added a bounded, redacted diagnostics export for local troubleshooting and expanded
  typed REST contracts, connector conformance, browser coverage, and critical backend
  coverage gates.
- Added transactional audit outbox delivery so security-relevant domain mutations and
  their audit records commit together and project idempotently.

### Changed

- Split large frontend runtime and connector-console modules, shared common action,
  request, SQL, result, editor, and live-console primitives, and added stale-response
  guards across asynchronous UI flows.
- Backup integration now requires protocol v2 capabilities. Upgrade the backup service
  to v0.2.0 before upgrading the AIPermission gateway.

### Fixed

- Canceled connector actions now finalize consistently, stale responses are rejected
  across scope changes, unchanged runtime revisions remain stable, and Vault lease
  repair runs only after audit recovery is available.
- Gateway shutdown, bounded HTTP failures, MCP authentication backoff, audit lifecycle
  accounting, diagnostic metadata, and encrypted database upgrade recovery now preserve
  explicit and reviewable failure states.

### Security

- Vault and backup mutations are bound to the transactional audit outbox, and connector
  action recovery is protected against ambiguous retries and stale completion responses.
- SQLCipher upgrades verify encrypted database compatibility, repository history is
  scanned for secrets, diagnostics have redaction regression tests, and CI maintenance
  checks enforce release integrity.

## [0.2.25] - 2026-08-11

### Added

- Added bounded S3 multipart upload and recursive file-transfer queues with progress,
  pause, cancellation, overwrite checks, and explicit count/byte limits.
- Added short-lived S3 download/upload URLs, object-version browsing and deliberate
  restore/delete actions, plus bucket lifecycle visibility and guarded policy
  replacement/deletion.
- Added automatic encrypted-backup retention with preview/final-version protection and
  provider quota/usage visibility.

### Changed

- Generalized connector file-transfer runtimes so SSH and S3 use the same queue
  lifecycle without moving connector-specific transport behavior into shared API code.

### Security

- Presigned URLs are limited to one exact key and at most one hour. Lifecycle
  replacement, lifecycle deletion, and version deletion are explicit destructive
  connector actions.

## [0.2.24] - 2026-08-11

### Changed

- Added one canonical release manifest plus a hygiene check that keeps the UI, frontend
  package, MCP package, MCP registry metadata, and changelog version aligned.
- Pull request CI now runs the Playwright browser smoke suite and retains its failure
  report for diagnosis.

### Fixed

- Connector action lifecycle updates now commit their canonical request and History
  projection together. Interrupted running actions recover as `outcome_unknown` so
  clients do not blindly retry a possibly completed remote side effect.
- Failed database rename operations reopen the original encrypted runtime, and failed
  imports remove or quarantine unusable targets instead of leaving a misleading occupied
  database name.

### Security

- Added a machine-readable native dependency inventory and a runtime assertion for the
  SQLCipher version embedded by the Go driver.
- Container publishing now scans the exact candidate registry digest before promoting
  that same digest to release tags, and requires the tagged source commit's CI checks to
  have passed.

## [0.2.23] - 2026-08-11

### Changed

- Split large backend and frontend modules by responsibility, established a frontend
  formatting baseline, and reduced the enforced React hook suppression budget to zero.
- Added a generated OpenAPI route inventory derived from backend route registration,
  with exact drift checks in the release gate.
- Added maintainer-triggered real-service connector conformance for Postgres, Valkey,
  RabbitMQ, and S3, plus project governance and ownership guidance.

### Fixed

- Gateway containers recover after host reboot, and owned console sessions now recover
  from missing, expired, or stale Vault authorization leases.
- Runtime audit records preserve target names, stale approval history remains
  synchronized, and connector restore uploads stream bounded payloads instead of
  buffering the full file in memory.

### Security

- Connector-owned HTTP transports ignore ambient proxy credentials, and approval
  execution freezes the validated credential material at claim time.
- Audit persistence failures are surfaced through degraded health state, while connector
  credential/output boundary regression tests guard against returning gateway-held
  secret values.
- CI blocks fixed HIGH/CRITICAL findings in locally built container images; publishing
  adds SBOM/provenance attestations and digest signing, while CI dependencies remain
  SHA-pinned.

### Maintenance

- Added source-size, coverage reporting, formatting, REST-contract, and frontend
  hook-debt checks to the maintenance/release workflow.
- Updated verified Go, frontend, MCP, and container dependencies through
  maintainer-authored commits.
- Documented the proposed transactional audit outbox boundary for a future atomic
  persistence implementation.

## [0.2.22] - 2026-08-03

### Added

- Added an isolated IMAP and SMTP Mail connector with bounded folder browsing, message
  search and reading, read-state changes, safe moves, archive and Trash workflows,
  compose, and reply.
- Added direct TLS and generic Over SSH transport support with independent IMAP and SMTP
  connection diagnostics.

### Changed

- Connector approval snapshots now bind generic execution context and dependent
  transport target/profile revisions, requiring fresh approval after drift.
- Mail content is rendered as bounded untrusted data, with complete outbound approval
  projections and explicit protocol/result limits.

### Security

- Mail authentication requires verified TLS; plaintext IMAP/SMTP transports, POP3, and
  attachment download are intentionally unsupported.
- Recipient, folder, message-reference, MIME, response-size, and timeout boundaries are
  revalidated during execution, while BCC remains envelope-only.
- SMTP acceptance uncertainty is reported without unsafe automatic retry hints.

### Maintenance

- Updated the pinned unprivileged nginx runtime image, Go compression library, Lucide
  icon package, and Playwright test package through maintainer-authored dependency
  commits.

## [0.2.21] - 2026-08-01

### Added

- Added exact single and selected remote backup deletion from Settings, with local
  record reconciliation after confirmed service deletion.
- Added encrypted per-service recovery baselines and an unlock-time warning when the
  remote backup service contains a newer recovery version.

### Changed

- Remote restore versions are grouped by source installation and show both relative and
  exact timestamps for safer recovery selection.
- Backup cleanup can combine explicit selection with the existing keep-last-N retention
  workflow.

### Security

- The backup service refuses to delete the final recovery version in a stream.
- Freshness baselines stay inside the encrypted local database and advance only after a
  successful upload or restore; sync checks never mark unseen backups as reviewed.
- Failed remote freshness checks are shown explicitly instead of being treated as an
  up-to-date result.

## [0.2.20] - 2026-07-31

### Added

- Added the provider-neutral AIPermission Backup protocol client for immutable encrypted
  `.aipdb` upload, version listing, download, and restore.
- Added first-run remote restore so a new machine can select a database stream and
  immutable backup version before any local database exists.
- Added explicit stream pruning that retains a chosen number of newest immutable
  versions and permanently removes only older versions.
- Added a separate self-hosted backup service with bounded authenticated APIs, atomic
  blob storage, checksums, non-root container hardening, and no gateway or connector
  execution surface.

### Changed

- Replaced Google Drive OAuth backup runtime support with the self-hosted AIPermission
  Backup provider. Existing Google provider secrets are cleared and their records
  archived during database migration.
- New remote providers start disabled and require an explicit protocol test and
  database-password verification before uploads are enabled.
- Restore now accepts an editable local database name and shows shortened stable stream
  identities so same-named databases remain distinguishable.

### Security

- Remote backup requires a stronger database password because possession of an encrypted
  remote file enables offline password guessing.
- The backup service receives encrypted database bytes and its own bearer token, never
  the database password, gateway vault key, decrypted contents, MCP tokens, connector
  credentials, SSH keys, or permission rules.
- First-run service URL, token, and database password remain transient request values
  and are not persisted in browser storage or a local provider record.

## [0.2.19] - 2026-07-31

### Added

- Added guarded Kafka / Redpanda `publish_message` writes with explicit partition
  selection, bounded keys, values, and headers, all-in-sync-replica acknowledgements,
  and no automatic topic creation.
- Added destructive `set_consumer_group_offset` control for one explicit topic
  partition, including a best-effort inactive-group guard, valid log-range validation,
  post-commit verification, and post-commit group-state reporting.
- Added local browser dialogs with explicit confirmation for publishing messages and
  changing inactive consumer-group offsets. Browser actions use the shared connector
  execution, history, and audit path; MCP calls continue to use token permissions and
  Prompt/Always policy.

### Security

- Publish approval previews expose content lengths instead of raw message bytes. Raw
  keys, values, and headers remain only in the encrypted execution envelope and are
  redacted from displayed request input.
- Offset changes require an inactive consumer group immediately before commit and are
  classified destructive. Kafka cannot provide an atomic member-join lock across that
  interval, so Prompt remains the recommended rule.

## [0.2.18] - 2026-07-31

### Added

- Added one built-in Kafka / Redpanda connector with Direct and Over SSH connection
  modes, multiple bootstrap brokers, optional TLS/custom CA, and optional PLAIN or SCRAM
  SASL credential profiles.
- Added read-focused actions for cluster metadata, topics, partitions, consumer groups,
  committed offsets, lag, and bounded message samples.
- Added a structured Kafka / Redpanda browser for topic and consumer-group inspection
  without joining a consumer group or committing offsets.

### Fixed

- Scoped local UI session and CSRF cookies by frontend port so development and stable
  localhost stacks can remain unlocked independently.

### Security

- Message sampling uses explicit partition assignment, strict record/byte/time bounds,
  no automatic topic creation, and no consumer offset commits.

## [0.2.17] - 2026-07-31

### Added

- Added Valkey compatibility to the built-in Redis connector, including Redis/Valkey
  product selection, server identity detection, Direct and Over SSH connections, and the
  existing bounded key browser/action surface.

### Changed

- Renamed the product-facing connector label to Redis / Valkey while preserving the
  existing `redis` connector kind, target refs, permissions, and actions.

### Security

- Bounded RESP line, bulk, array, nesting, total-byte, and total-value parsing before
  allocation so malformed Redis-compatible responses fail closed.

## [0.2.16] - 2026-07-30

### Added

- Added Project Vault for encrypted project-scoped secret inventory, metadata,
  expiration tracking, local reveal, generated-value replacement, and default
  connector-session bindings.
- Added an Always-only project capability for metadata listing plus Prompt and Always
  capabilities for secret generation and exact-session environment application without
  returning secret values through MCP.
- Added local Vault approval, request history, audit events, exact-session authorization
  leases, peer-identity binding, and context-drift invalidation.

### Security

- Bound Vault session use to the exact token, workspace, runtime, session, generation,
  approval context, environment content, target/profile state, and connector peer
  identity.
- Added framed one-time SSH environment delivery, terminal echo suppression, exact-value
  transcript and manual-history redaction, strict action input normalization, bounded
  approval lifetimes and request creation, and fail-closed session cleanup.

### Documentation

- Added the Project Vault guide and aligned the MCP, REST, architecture,
  connector-development, credential, storage, and threat-model documentation.

## [0.2.15] - 2026-07-29

### Changed

- Updated the MCP SDK to 1.30.0 and refreshed its production dependency tree.
- Migrated the frontend from `react-router-dom` 7 to the patched `react-router` 8 line,
  and updated verified React, Radix, Lucide, Tailwind, and Playwright dependencies.
- Refreshed the pinned Go builder image and upgraded `actions/checkout` to 4.4.0 across
  CI and publishing workflows.
- Kept Monaco Editor on 0.53 because 0.56 removes the worker import path used by the SQL
  console and currently breaks the production build.

### Security

- Updated `github.com/pkg/sftp` to 1.13.11, which bounds untrusted SFTP
  extended-attribute preallocation and prevents a peer from forcing excessive memory
  allocation.
- Removed the current production npm audit findings from the frontend and MCP package
  dependency trees.

### Tests

- Re-ran backend tests, race detection, vet, `govulncheck`, frontend runtime tests,
  production build, Playwright scenarios, MCP tests/build/package validation, production
  npm audits, and a full Docker Compose rebuild.

## [0.2.14] - 2026-07-18

### Added

- Added ClickHouse as a built-in connector with native-protocol Direct and generic Over
  SSH connection modes.
- Added structured ClickHouse actions for visible databases, tables, ordered column
  metadata, and bounded read-only analytics queries.
- Added a ClickHouse Console workspace with database/table browsing, SQL completion,
  structured results, raw JSON, and per-session activity.

### Changed

- Postgres and ClickHouse now share connector-owned SQL console primitives, read-only
  SQL validation, bounded result serialization, and reusable network transport fields
  without adding database-specific branches to generic pages.
- Updated Go cryptography dependencies, including `golang.org/x/crypto` 0.54.0.

### Security

- ClickHouse queries reject writes and multi-statement SQL, run with `readonly=1`, and
  enforce timeout, row, cell-size, and serialized-output limits. Database grants remain
  the primary least-privilege boundary.
- Kept Monaco Editor on the audit-clean 0.53 line because the proposed 0.55.1 update
  currently introduces a production DOMPurify advisory.

### Tests

- Added ClickHouse Direct, Over SSH, TLS, metadata, query, parser, output-limit,
  connector registration, and shared SQL console coverage.

## [0.2.13] - 2026-07-11

### Added

- Added first-class local projects for organizing connector targets, with a protected
  `Ungrouped` project for existing and unassigned targets.
- Added project management and grouped connector navigation in Connectors and Console,
  including project-aware search and stable collapsed groups.
- Added project-scoped MCP visibility controls to Tokens and the Console token panel.

### Changed

- MCP target discovery now returns only targets in projects enabled for the calling
  token, while preserving existing target/profile/action grants when a project is
  temporarily disabled.
- History and Audit Logs now store project snapshots and support project filters; the
  History search and filter controls use a clearer two-row layout.
- Updated the Go toolchain baseline to 1.26.5.

### Security

- Project scope is an additional local visibility boundary above connector action
  permissions. It does not introduce team RBAC, multi-user isolation, remote hosting, or
  a tenant security boundary.

### Maintenance

- Updated Lucide, React Router, and Tailwind frontend patch dependencies while keeping
  the production dependency audit clean.

### Tests

- Added migration, project lifecycle, target assignment, token project scope, MCP
  visibility, history/audit snapshot, and connector project-edit coverage.

## [0.2.12] - 2026-06-30

### Changed

- Improved S3 MCP/operator guidance so agents use `bucket_info`, `list_objects`,
  directory `browse_input`, pagination `cursor`, object metadata, bounded
  download/upload, rename, and delete actions more reliably.
- Added S3 list response hints for folder browsing and pagination, including
  `assistant_hints`, per-folder `browse_input`, and `next_page_input`.
- Clarified S3 action input descriptions for prefix browsing, search, pagination,
  metadata-first reads, bounded object content, overwrite behavior, and destructive
  deletes.

### Security

- S3 operator guidance now explicitly warns agents not to put access keys, signed URLs,
  reusable tokens, or other secret material into connector action input.

### Tests

- Added S3 connector coverage for directory browse hints and pagination follow-up input.

## [0.2.11] - 2026-06-29

### Added

- Added S3 as a built-in connector with Direct and Over SSH connection modes.
- Added S3-compatible bucket browsing, object metadata reads, bounded object
  download/upload, object rename, and explicit delete actions.
- Added an S3 Console browser template for object search, metadata inspection, download,
  upload, rename, and delete flows.

### Changed

- Moved live-console recovery action selection into connector frontend templates, so
  recovery UI is connector-defined instead of SSH-specific.
- Moved connector-provisioned credential metadata ownership behind connector contracts,
  so managed profile details no longer live in generic API handlers.
- Removed a stale Docker-shaped target-operation request DTO from generic connector
  target handlers.

### Fixed

- Direct connector TCP dials now prefer IPv4 before IPv6 fallback when both are
  available, avoiding Docker-hosted gateway timeouts on providers whose IPv6 endpoint is
  unreachable from the container network.

### Tests

- Added S3 connector tests for SigV4 request signing, path escaping, upload preview
  redaction, list filtering, and overwrite protection.
- Added direct connector dial ordering coverage for dual-stack DNS responses.
- Added backend and frontend boundary checks that guard against connector details
  leaking back into generic API handlers or generic Console code.

### Security

- S3 credentials are stored only as connector credential profile secrets, and object
  content upload/download actions are bounded by connector size limits.

## [0.2.10] - 2026-06-23

### Added

- Added Kubernetes as a built-in connector that runs bounded `kubectl` templates through
  an SSH transport profile.
- Added Kubernetes Console resource tabs for workloads, pods, services, ingress, nodes,
  and events, with namespace filtering and selected-resource details.
- Added Kubernetes MCP actions for cluster version, namespaces, workloads, pods,
  services, ingress, nodes, warning-first events, resource describe, and bounded pod log
  tails.
- Added live Kubernetes pod console sessions that reuse the same terminal component as
  SSH and Docker console while entering one selected pod/container.
- Added an explicit `rollout_restart` deployment action for operator-approved restarts.

### Fixed

- Fixed manual command history completion for Kubernetes/BusyBox-style path prompts such
  as `/ #` and `/app $`, so pod-console commands no longer remain stuck as `running`.

### Security

- Kubernetes does not expose raw `kubectl`, manifest apply/edit/delete, pod deletion,
  scaling, or Secret value browsing.
- Kubernetes profiles scope access by namespace visibility, and pod logs remain bounded
  by requested tail limits.
- `rollout_restart` is the only Kubernetes write action in this release and is still
  governed by token/action permissions and approval policy.

## [0.2.9] - 2026-06-23

### Added

- Added Docker inventory actions for scoped image, network, and volume reads.
- Added Docker Console inventory tabs for Containers, Images, Networks, and Volumes,
  with searchable metadata and raw JSON copy/search views.
- Added Docker container health and Docker Compose project/service metadata to scoped
  container list output when Docker labels/status expose it.
- Added scoped `container_exec` for bounded non-interactive commands inside one visible
  container.
- Added live Docker container console sessions that reuse the same terminal component as
  SSH console while entering a selected container through the configured SSH transport
  profile.
- Added a tag-triggered GitHub Actions workflow for publishing ready-to-run backend and
  frontend images to GitHub Container Registry.
- Added `docker-compose.release.yml` for users who want to pull published images instead
  of building from source.

### Security

- Selected Docker credential profiles only expose images used by visible containers, and
  derive networks/volumes from scoped container inspect output.
- Prebuilt-image Compose keeps the UI port bound to `127.0.0.1` and does not change
  AIPermission's local-only gateway boundary.
- Docker `container_exec` and live container console are scoped to one visible
  container. Docker still does not expose arbitrary host-level Docker commands,
  container/image removal, prune, or raw Docker command execution.

## [0.2.8] - 2026-06-23

### Added

- Added Docker as a built-in connector that runs bounded Docker CLI templates through an
  SSH transport profile.
- Added Docker actions for version metadata, scoped container listing, redacted inspect
  metadata, bounded log tails, and explicit start/stop/restart lifecycle operations.
- Added a Docker Console container browser with profile-scoped container lists, logs,
  inspect output, and lifecycle confirmation dialogs.
- Added Docker credential profile scopes so a token can be limited to all containers,
  selected container names/IDs, or name patterns.

### Changed

- Added a generic connector command transport capability so structured connectors can
  run reviewed command templates through connector transports without importing
  SSH-specific code.

### Security

- Docker does not expose arbitrary `docker exec`, container/image removal, prune, shell,
  or raw Docker command execution in the 0.2.8 MVP.
- Docker inspect output masks container environment values before returning structured
  output to UI, MCP, history, or audit.
- Container-targeted Docker actions resolve the requested container first and enforce
  the credential profile scope before reading logs, inspecting metadata, or running
  lifecycle changes.

## [0.2.7] - 2026-06-19

### Added

- Added a Settings-only realtime Maintenance Console for local diagnostics inside the
  gateway runtime.
- Added backup provider metadata storage with Google Drive as the first provider type.
- Added Settings UI for adding, editing, disabling, and archiving remote backup provider
  metadata.
- Added Google Drive device-code authorization that stores OAuth token payloads as
  encrypted backup-provider secrets.
- Added Google Drive encrypted database backup upload from the Settings provider row,
  with local backup record metadata for uploaded `.aipdb` files.
- Added remote backup record download and restore-as-new-local-database flows for
  connected Google Drive providers.
- Added upload confirmation that shows the encrypted snapshot size before a remote
  backup upload starts.

### Security

- Maintenance Console is local UI only, unavailable to MCP, and audits terminal
  lifecycle events without exposing the local terminal through MCP.
- Backup providers are storage metadata only. They do not receive MCP tokens, connector
  credentials, or the database password.
- Google Drive OAuth tokens are encrypted with the local vault and are not returned by
  provider list/detail responses.
- Google Drive uploads send encrypted `.aipdb` snapshots only; database passwords and
  connector credentials are not uploaded.
- Google Drive restores verify stored size/checksum metadata and never overwrite the
  currently open database.

## [0.2.6] - 2026-06-18

### Added

- Added RabbitMQ as a built-in connector with Direct and Over SSH connection modes
  through the shared connector permission, approval, history, and audit pipeline.
- Added RabbitMQ actions for overview metadata, visible vhosts, bounded queue lists,
  queue details, bindings, bounded message peeking with `ack_requeue_true`, and explicit
  message publishing.
- Added a RabbitMQ Console queue browser with vhost filtering, queue counters, binding
  inspection, and bounded message payload previews.
- Added connector form host reachability checks that run four TCP dials directly or
  through the selected SSH transport profile.

### Changed

- Direct connector targets can use `host.docker.internal` to reach services running on
  the same Linux host as AIPermission Docker.

### Security

- RabbitMQ message preview actions are bounded by count and payload truncation limits.
  `publish_message` is a separate write action; purge, ack, nack, delete, or other
  destructive queue operations are intentionally not part of the 0.2.6 MVP.

## [0.2.5] - 2026-06-18

### Added

- Added Over SSH connection mode for Postgres connector targets, using the same generic
  connector network transport introduced for Redis.
- Added Postgres target form controls for selecting an SSH transport profile when the
  database is reachable only from a remote server.

### Changed

- Postgres actions, connection tests, managed role provisioning, backup, and restore now
  use the connector transport abstraction instead of a direct-only code path.

## [0.2.4] - 2026-06-18

### Added

- Added Redis as a built-in connector with Direct and Over SSH connection modes through
  the shared connector permission, approval, history, and audit pipeline.
- Added Redis actions for `ping`, `info`, bounded `scan_keys`, `get_key`, `set_string`,
  `expire_key`, and explicit `delete_keys`.
- Added a Redis Console key browser with SCAN search, key inspection, string editing,
  TTL updates, and multi-key delete.

### Changed

- Added a generic network transport capability so protocol connectors can open direct
  TCP connections or delegate to reviewed connector transports such as SSH without
  importing SSH-specific code.
- Updated connector docs and MCP package docs to describe Redis as a built-in connector.

## [0.2.3] - 2026-06-18

### Changed

- Console now shows one row per connector target and keeps credential profiles
  selectable from the target header, so a Postgres target with multiple profiles no
  longer looks like multiple databases.
- Connector token controls now infer Basic, Grouped, or Advanced permission mode from
  the current action rules instead of always returning to a fixed default view.
- Postgres Console now keeps profile-scoped sessions and activity easier to inspect when
  switching between credential profiles.

### Added

- Added Postgres recent-query shortcuts so prior SQL can be loaded back into the editor.
- Added Postgres request-level SQL actions for loading or copying the SQL from an
  inspected request.

## [0.2.2] - 2026-06-17

### Added

- Added Postgres managed database-user provisioning with schema/table/column scope
  selection, random password generation, encrypted credential storage, and managed-role
  cleanup when the profile is deleted.
- Added Postgres SQL backup/download and restore/upload through a local UI operator flow
  backed by `pg_dump` and `psql`.
- Added a Postgres schema/table browser in the Console SQL surface.

## [0.2.1] - 2026-06-16

### Changed

- Refreshed backend and frontend Docker base image digests through Dependabot.
- Refreshed the frontend npm dependency group through Dependabot.
- Updated the MCP package metadata to 0.2.1.
- Kept `monaco-editor` on the audit-clean 0.53 line until the newer line clears its
  transitive `dompurify` advisory.

### Security

- Updated `golang.org/x/crypto` to 0.53.0.
- Updated MCP transitive `hono` resolution to a non-vulnerable version.
- Hardened SSH connector integer config parsing with native-int bounds checks to close
  CodeQL narrowing-conversion findings.

## [0.2.0] - 2026-06-16

### Added

- Added the connector-native runtime model: connector target, credential profile, token
  action permission, approval policy, connector execution, history, and audit.
- Added Postgres as the first structured connector, with schema discovery, table
  metadata, and bounded read-only SQL actions through database credential profiles.
- Added connector UI templates for target forms, credential forms, connector list
  operations, Console activity surfaces, and toolbar actions.
- Added connector approval-context snapshots that cover target/profile metadata,
  credential revisions, connector action definitions, permission state, and prepared
  payload hashes before approval execution.
- Added typed connector adapter contracts for runtime-backed capabilities such as live
  terminals, file transfer, credential resources, and target lifecycle cleanup.

### Changed

- SSH now uses the same connector target/profile/action vocabulary as structured
  connectors, while keeping its live terminal and file transfer adapter surfaces.
- Frontend connector templates now validate required slots, model exports, and metadata
  icons during tests so new connector kinds cannot silently ship partial UI contracts.
- Postgres targets now default to `ssl_mode=require`; weaker modes remain an explicit
  local-lab choice.
- Reset the local schema as a clean 0.2 connector-native baseline while the project is
  still pre-1.0.
- Pre-0.2 preview databases are not migrated automatically by the normal gateway. Create
  a fresh 0.2 database, or run the local migration helper with `docker compose --profile
  migrate up -d --build migration` and open `http://localhost:3211` to migrate important
  0.1.x data into a new 0.2 database.

### Security

- Structured connector outputs use shared redaction before MCP responses, history, and
  audit persistence.
- Target schemas reject secret fields; credential profile schemas own encrypted secret
  material.
- Stale approval requests now record a coarse drift reason such as token, permission,
  target, profile, action definition, or payload drift.

## [0.1.14] - 2026-06-10

### Changed

- Relicensed AIPermission to AGPL-3.0-only from this release onward.
- Documented that versions up to and including v0.1.13 remain available under their
  original MIT license.

## [0.1.13] - 2026-06-09

### Added

- MCP `exec` can now run the same command across multiple visible SSH targets, while
  preserving one request, approval decision, audit record, output, and error per target.
- MCP `read_console` can now inspect several always-run SSH target consoles in one
  read-only call.
- MCP command responses can include basic policy warnings for common high-risk command
  patterns such as destructive file operations, package/service changes, firewall
  changes, credential reads, and container/cluster destructive actions.

### Changed

- The MCP bridge keeps multi-server command execution on the existing `exec` tool
  instead of exposing a separate bulk command tool, reducing tool-list clutter for AI
  clients.
- Operator instructions now tell AI clients to use multi-server `read_console` after
  multi-server `exec` when several always-run targets are still running.

### Fixed

- NAS/appliance console prompt detection now recognizes bracket-style shell prompts such
  as `[~] #`, improving QNAP-style interactive shell compatibility.

### Security

- Multi-server MCP execution does not bypass per-server token permissions,
  approval-required prompts, blocked rules, approval-context drift checks, or
  token-scoped history.
- Policy warnings are best-effort UX safety rails and do not replace local operator
  approval, token permissions, or command review.

## [0.1.12] - 2026-06-09

### Added

- Console Bulk command can run one shell command across multiple selected servers from
  the local UI.
- Bulk command requires an explicit confirmation phrase, records one manual command
  request per target server, and runs through the same persistent SSH console path as
  normal Console commands.
- Bulk command results show per-server status and selectable captured output, with a
  compact target search for larger server lists.

### Security

- Bulk command remains local UI only and requires the existing unlocked UI session plus
  CSRF checks.
- Bulk command history rows are stored as manual command requests without MCP token
  identity, so MCP approval and token-scoped history semantics stay separate.

## [0.1.11] - 2026-06-08

### Added

- SSH connectors can now store optional advanced startup settings for NAS and appliance
  targets that show an interactive menu before a normal shell.
- Advanced startup settings support post-connect input, such as `q\n` for some QNAP
  menus, and an optional forced shell command for compatibility targets.

### Changed

- Updated the Go toolchain/Docker builder image, frontend dependencies, and the SFTP
  dependency after local test verification.

### Fixed

- Windows checkouts now preserve LF line endings for Docker shell scripts, with a
  hygiene check to catch CRLF entrypoint regressions.
- Console recovery banners now distinguish manual console commands from MCP/AI commands.
- SSH command execution, Docker checks, and connection tests now share clearer timeout,
  connection refused, authentication, and host-key error messages.
- Approval Run now checks SSH session readiness before closing the prompt, so
  unreachable/offline targets surface a readable terminal error immediately.
- Basic redaction no longer masks normal shell `PWD=/path` output while still masking
  lowercase `pwd=...`, password, token, API key, bearer token, and private-key patterns.
- README and operator instructions now clarify that `list_servers` is permission-scoped
  and not a live SSH health check.

## [0.1.10] - 2026-06-08

### Added

- Console now shows active long-running MCP commands for the selected server, including
  running age, token label, command, reason, and stuck-session guidance.
- Local operators can restart a server-scoped persistent console session from the
  Console UI when it appears wedged.
- MCP running-request hints and operator instructions now document the recovery
  sequence: poll `get_request`, inspect `read_console`, then use
  `restart_console_session(server_id)` when no useful progress is visible.

### Fixed

- Hide internal persistent-console prelude lines from the live console and MCP command
  output when a PTY echoes setup commands.
- MCP server-list hints now clarify that `list_servers` is permission-scoped; agents
  should rely on `exec` dial, timeout, SSH authentication, and host-key errors for
  current reachability.

## [0.1.9] - 2026-06-07

### Added

- Pending MCP connector-action approvals now store an approval-context snapshot covering
  the token, target/profile/action permission, target metadata, credential profile
  revision, connector action definition, MCP tool metadata, and prepared payload hash.
- Approval dialogs show how long ago the request was created.
- MCP clients can restart a stuck persistent console session for a visible server,
  causing the next `exec` call to open a fresh SSH session.

### Security

- If a pending command's approval context changes before the operator clicks Run,
  AIPermission marks the request `stale` and requires the AI to submit a fresh request
  instead of executing an old approval.

### Fixed

- MCP command execution is more resilient when a persistent console session is closed or
  restarted while a command request is still running.

## [0.1.8] - 2026-06-07

### Added

- Token/server permissions can now have an optional `expires_at` timestamp for temporary
  maintenance grants.
- Console token controls can turn active Prompt or Always permissions into 1-hour,
  4-hour, or 1-day temporary grants.
- The Console always-run warning shows a countdown when an active `always_run` grant is
  temporary.
- MCP `list_servers` includes `expires_at` for temporary grants and omits expired
  grants.

### Security

- Expired token/server permissions are not treated as effective by MCP command, console,
  file-transfer, or server-list permission checks.
- Permission expiration is a local safety rail for temporary maintenance windows. It
  does not change the local-only threat model or make exposed gateway ports safe.

## [0.1.7] - 2026-06-06

### Added

- MCP file transfer status tools for token-scoped transfer and batch metadata.
- MCP remote directory browsing and remote download queue start tools for `always_run`
  server permissions.
- MCP `save_file_download` for writing completed gateway downloads to an explicit local
  path from the local MCP process.
- MCP `upload_files` for uploading explicitly named local files to a remote directory
  through the gateway.
- MCP transfer start tools now support `approval_required` server permissions by
  creating a local Transfer Center approval queue where selected files can be approved
  and the rest rejected with a note.
- MCP pause, resume, and cancel tools for active transfer queues.
- Transfer Center in the local UI for monitoring active and recent UI/MCP transfer
  queues without keeping the original dialog open.

### Security

- MCP transfer responses never include local temporary paths, local upload paths,
  archive staging paths, or file contents.
- MCP direct transfer tools require explicit local paths. `always_run` starts queues
  immediately, while `approval_required` creates a local Transfer Center approval queue
  before touching the remote server.

## [0.1.6] - 2026-06-06

### Added

- Queued SSH/SFTP uploads and downloads from the local Console UI.
- Multi-file upload queue with per-file ordering, removal, overwrite confirmation, live
  progress, speed, and ETA.
- Multi-file remote download queue with zip packaging after remote downloads complete.
- Pause and resume controls for active transfer queues while the gateway process remains
  running.
- Batch transfer REST endpoints for queue status, pause, resume, cancel, and final
  download delivery.
- Duplicate remote paths are rejected before transfer start; download zip entries get
  stable numeric suffixes when remote basenames collide.

### Security

- File contents remain outside SQLCipher. Transfer history stores metadata, status,
  progress, checksum, path, and errors only.
- Uploads are written to a temporary remote file first and moved into place only after
  the upload completes, so canceling an upload does not leave a partial target file
  behind.
- Download batches are capped at 1 GiB total remote file size.
- Pause/resume is intentionally process-local. If the gateway process, Docker container,
  or computer restarts, unfinished transfer queues should be started again instead of
  resumed from old local state.
- MCP file-transfer tools remain intentionally unavailable while UI transfer safety and
  audit semantics are dogfooded.

## [0.1.5] - 2026-06-05

### Added

- SSH file transfer history model for upload/download metadata, status, progress,
  checksums, and errors.
- UI-driven single-file upload over SFTP from Console.
- UI-driven single-file remote download over SFTP, with browser download after
  completion.
- Remote file/folder browser for selecting SFTP upload directories and download files
  from the local UI.
- Cancel support for pending or running UI file transfers.
- Explicit overwrite confirmation before replacing an existing remote file.
- File Transfer History tab with pagination, search, server/status/direction filters,
  live progress display, and detail dialog.

### Security

- File contents are never stored in SQLCipher. Uploads are staged in a private temporary
  directory and removed after the remote transfer finishes or fails.
- Remote downloads are staged in a private temporary file and served through the browser
  only after the transfer reaches `completed`; temporary downloads are short-lived.
- File transfer is currently exposed through the local web UI only. MCP file-transfer
  tools are intentionally not exposed in this release.

### Notes

- This is a conservative single-file MVP. Directory transfer, recursive copy, remote
  glob expansion, resumable transfers, and SSH-agent/ProxyJump based transfer transports
  are still future work.

## [0.1.4] - 2026-06-05

### Added

- Manual Console command logging for simple terminal input. Manually typed or pasted
  commands are recorded as `source = manual` without changing normal terminal behavior.
- Best-effort output capture for simple manual commands. When the shell prompt returns,
  History records captured output and marks the command `completed` or `canceled`.
- History source filters and badges for MCP and manual command records.
- `source`, `tracking_reason`, and `output_truncated` command request fields for manual
  command records.

### Security

- MCP request list/detail APIs explicitly remain scoped to MCP-origin command requests
  so manual History rows cannot leak through MCP tools.

### Notes

- Interactive commands, nested shells, heredocs, and unsafe control sequences are stored
  as `untracked` best-effort records. Arrow/history recall is stored with a placeholder
  command because the terminal does not send the recalled command text; simple recalled
  commands may still capture output when the prompt returns, while ambiguous interactive
  recalled commands are left `untracked`.
- This release does not install shell hooks, append hidden command suffixes, or infer
  shell history recall from arrow keys.

## [0.1.3] - 2026-06-04

### Added

- Existing SSH private key import with optional import-time passphrase handling.
- SSH host import from OpenSSH config files and pasted config content for prefilling
  server records.

### Changed

- Terminal-like command, output, log, and setup code blocks now share consistent
  typography.
- SSH host import avoids sending `IdentityFile` paths into server descriptions and
  reports `ProxyCommand` without returning the raw command.
- SSH config parsing follows OpenSSH-style first-value-wins behavior for matching `Host`
  blocks.
- Imported RSA private keys must be at least 2048 bits.

## [0.1.2] - 2026-06-03

### Added

- History labels for tagging command requests, filtering history by label, and cleaning
  up labels from Settings without deleting history records.
- On-demand Docker quick checks from the Servers page for current container status and
  exposed ports.

## [0.1.1] - 2026-06-02

### Added

- Manual GitHub release update check in the in-app changelog dialog.
- Bulk token permission updates for applying one rule to every server.
- Optional approval-run notes that are delivered back to the AI after approval.

### Changed

- Console side panels can collapse for narrower screens.
- Browser title now shows the MCP runtime state and active database name after unlock.
- Console server status dots now reflect live session, pending, and running state
  instead of decorative window controls.
- Database deletion now requires a second confirmation dialog with the current database
  password.

### Notes

- This release is focused on dogfooding polish after the first public RC.

## [0.1.0-rc.1] - 2026-06-02

### Added

- Local-only Docker gateway with React UI on `http://localhost:3210`.
- SQLCipher-encrypted named `.aipdb` databases with unlock, switch, import, backup,
  rename, delete, and password-change flows.
- Gateway-owned SSH key generation, public-key install commands, SSH host fingerprint
  approval, and server records.
- Token-scoped MCP command execution with `always_run`, `approval_required`, and
  `blocked` rules.
- Persistent web console sessions, live output streaming, approval dialogs, messages,
  command history, and audit logs.
- `@aipermission/mcp` package with init and operator-skill installer for common AI
  clients.
- Security controls for local-only HTTP boundaries, UI session cookies, CSRF, redaction
  rules, reusable-token opt-in, and supply-chain checks.

### Security

- SSH private keys and reusable token values stay inside the local encrypted gateway.
- API tokens are stored as hashes and shown once by default.
- Approval-required raw commands are encrypted separately so display redaction cannot
  mutate execution payloads.
- `read_console` requires `always_run` permission to avoid exposing unrelated manual
  transcripts to approval-only tokens.

### Notes

- This RC is a local developer gateway, not a remote DevOps platform.
- Do not expose the UI/API on a LAN or the public internet.
