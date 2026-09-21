# Development Testing

Use the root `Makefile` for the common verification set.

Direct backend tests require a C compiler and OpenSSL 3 development headers
because the pinned SQLCipher wrapper uses CGO. Debian/Ubuntu contributors can
install `build-essential libssl-dev`; the backend Docker build installs the
same native dependency itself. CI uses the shared
`.github/actions/setup-backend-native` action so all Go jobs stay aligned.

## Quick Checks

```bash
make -f Makefile test
make -f Makefile build
make -f Makefile audit
```

## Release Candidate Checks

```bash
make -f Makefile release-check
```

This runs:

- repository secret, line-ending, source-size, and frontend hook-debt budgets
- pinned Gitleaks scanning across current files and complete Git history, with
  exact synthetic-fixture fingerprints in `.gitleaksignore` and a narrow
  semantic allowlist for generated SHA-256 recipe digests in `.gitleaks.toml`
- a fast tracked-file pattern scan; test sources with synthetic secret-shaped
  fixtures are intentionally skipped there and remain covered by the
  full-history Gitleaks scan
- a scheduled informational issue for direct npm majors and Go majors reported
  on an existing module path; path-changing Go majors such as `/v2` remain a
  manual maintainer review, and the workflow never creates or merges
  bot-authored dependency commits
- canonical release-note artifact, release-version, and native-dependency
  inventory consistency checks
- generated OpenAPI route and typed-schema drift
- architecture guards that keep API child packages transport-only, prohibit
  direct SQL and raw workspace-scope consumption in production API code, trace
  the OpenAPI generator to one canonical route source, and keep backend,
  frontend, and documented connector catalogs aligned
- architecture guards that reject production API type re-exports, mutable
  function/value facades, and every API-specific source, package, function, or
  dependency fan-out budget override
- backend unit tests with an aggregate summary and reviewed floors for auth,
  permission, approval, Vault, session injection, target lifecycle, and audit
  outbox packages
- deterministic encrypted recovery drills for backup/import, wrong-password,
  migration-fixture, restart, and stored gateway-secret continuity
- bounded fuzzing for approval contexts, SQL safety, redaction, Redis RESP,
  transfer paths, backup metadata, and connector payload normalization
- backend race tests
- backend vet
- backend govulncheck
- frontend tests
- Playwright policy validation that rejects skipped/fixed scenarios, requires
  the complete smoke, accessibility, high-risk, real-backend, and responsive
  viewport manifests to remain discoverable, and rejects removal of a
  base-branch gate in the same change
- frontend suite-manifest validation that discovers production owners using
  request/generation guards, abort controllers, polling timers, or sockets and
  requires each owner to map to a test that reaches it through the real import
  graph; this is a discovery/ownership gate, while cancellation and stale-result
  behavior remain explicit assertions in the mapped tests
- a trusted-base-ratcheted manifest for critical synchronous frontend behavior,
  including unlock, restore, transfer confirmation, Vault approval, maintenance,
  and token-secret dismissal; removing a test requires a replacement that still
  reaches the protected owner through the import graph
- frontend duplicate-block comparison against the base Git revision
- backend and MCP baseline-based duplicate-block comparison that rejects new
  meaningful clones; existing clones are recorded without retroactive CI failure
- TypeScript checks for connector action, permission, approval, Vault, and
  session contracts; untrusted HTTP responses are still validated at runtime
- a measured initial JavaScript budget with the maintenance terminal loaded
  only when Settings opens it
- frontend per-file coverage floors for connector permission editing, shared
  connector action and target/profile lifecycles, approval dialogs, and console
  page-state boundaries
- frontend production build
- frontend Playwright browser smoke plus explicit accessibility and high-risk
  workflow gates for keyboard focus, responsive unlock/setup, database import,
  token permissions, Prompt approval, structured session isolation, live-console
  reconnect, transfer cancellation, mobile navigation, Console drawers, and
  viewport-contained permission dialogs
- frontend Playwright lifecycle coverage against a real encrypted backend for
  Prompt approval, completion, stale-context rejection, lock/unlock, and restart
- explicit frontend async-state ownership coverage for stale completion,
  scoped cancellation, target/profile changes, WebSocket closure,
  reconciliation, and transfer ownership
- frontend production npm audit
- MCP package tests
- MCP package build
- MCP production npm audit
- MCP package dry pack
- unscoped placeholder package dry pack

The reviewed backend coverage floors are enforced by
`backend/cmd/coveragecheck` after the full package test run. That command is the
consumer; `maintenance-policy.json` is the single source of truth for numeric
thresholds. Both `internal/*` and executable `cmd/*` packages are inventoried.
The policy protects transport, gateway-owner, audit, connector, session, token,
Vault, storage, and command packages. Runtime scopes and critical owner floors
include failure-path tests; a new security-sensitive package must be added once
its baseline coverage is established.

Linux coverage inventory and floors are complemented by native
`windows-latest` and `macos-latest` backend jobs. They execute platform behavior
tests rather than merely cross-compiling their binaries, first compiling and
starting the complete `_test.go` graph with an empty test selection and then
requiring package-bound pass events for every named behavior test. Each mapped
platform source is checked against its own native coverage floor. The Linux job
also cross-builds the complete Windows source graph. Active host, Windows, and
macOS source inventories are merged. The tagged `cmd/e2e` browser harness is also
inventoried explicitly and excluded only while it belongs exclusively to the
declared `linux-e2e` build context. If it enters the host production graph, the
coverage gate fails even while its package remains listed as an exclusion.
Executable Windows and macOS files must be mapped in
`backendCoveragePlatformFiles` with their exact `//go:build` constraint, native
required tests, and coverage floor instead of disappearing from coverage
silently. Platform mappings and command exclusions are one-time ratcheted
exceptions. A new platform mapping is accepted only when the same reviewed
policy change registers exact native runtime evidence, requires 100% coverage
for the mapped source, and leaves every other exception budget unchanged.
Keep all checks required: platform-tagged ownership behavior must not be
represented as Linux coverage, and `go test -exec=true` alone is rejected as
behavior evidence.

Repository tooling tests are recursively discovered and must exactly match the
ratcheted `toolingTestFiles` inventory beneath explicit `toolingTestRoots`.
Discovery uses the same `.test`/`.spec` markers and JavaScript extensions as the
source classifier, so changing a suffix cannot silently bypass execution.
Native platform behavior tests similarly come from `windowsRuntimeTests` and
`darwinRuntimeTests`; each runner rejects execution on any other operating
system and requires one package-bound pass event per entry.
Deleting a test together with its manifest entry therefore fails the ratchet.
Required-check and command moves use a two-change authorization flow: first add
the exact migration while the old gate remains active, then move the gate only
after that migration exists in the trusted base revision.
Source-owner depth migrations use the same two-change rule after initial policy
bootstrap: authorize the exact depth and cap transition first, then apply it
from a later trusted base. Obsolete platform mappings and command exclusions
may be removed without weakening the additions-only exception ratchet.

Coverage floors intentionally trail the measured baseline by a small margin so
toolchain-only statement shifts do not create noise. Critical package floors
cannot be removed or decreased by the maintenance ratchet; raise them
incrementally when a release adds behavioral coverage.

The frontend coverage gate uses per-file V8 thresholds rather than one broad
aggregate percentage. This keeps a well-covered utility from masking a weak
authorization or session surface. Every production JavaScript module is a
coverage owner unless it is an explicitly listed generated or test-support
file. The checked baseline is compared with the base Git revision, so a change
cannot weaken its baseline in the same pull request. New modules must meet the
full floor; an existing module below the floor must improve by at least one
percentage point whenever it changes, until it reaches the floor.
Changed-coverage runs allocate an isolated temporary report directory, so
parallel or interrupted checks cannot reuse stale coverage artifacts. The
accepted bootstrap is pinned by both commit and Git tree, preserving the same
boundary when a rebase merge rewrites commit identities. Missing or malformed
base state fails closed, and untracked production owners are included before
the first commit. Baseline updates preserve both stronger accepted metrics and
the next required ratchet; the normal read-only gate must still pass afterward.
The IndexedDB-backed local action retry ledger has a separate Node coverage
gate that checks every retry module independently, so aggregate coverage or a
well-tested sibling cannot hide an under-tested storage boundary.

Playwright release gates run with retries disabled and reject committed
`test.only` calls in CI. A flaky first attempt is therefore a failure, while
failure traces remain available for diagnosis. High-risk route fixtures assert
the HTTP method and request body, and responsive accessibility checks run after
every tested unlock/setup tab transition. The checked manifest is ratcheted
against the base Git revision, so deleting both a critical test and its current
manifest entry cannot make the same pull request pass.

The real-backend browser test runs the production API, SQLCipher database,
UI-session authentication, CSRF, connector permission, approval, and history
paths. Its deterministic in-process connector replaces only the remote service,
so it can assert lifecycle behavior without network flakiness.

Run recovery and fuzz gates independently while developing:

```bash
make -f Makefile recovery-drill
make -f Makefile bounded-fuzz
```

Normal `go test` runs the committed fuzz seed corpus. `make -f Makefile bounded-fuzz` also
mutates each security boundary for 1,000 executions by default. Maintainers may
set `AIPERMISSION_FUZZ_TIME` to an integer execution count ending in `x`, or a
millisecond/second duration capped at 30 seconds, for a different local pass.
The bounded runner uses one fuzz worker so CI results remain reproducible.

The required connector conformance workflow exercises ClickHouse, Postgres,
Valkey, RabbitMQ, and S3 against disposable pinned service containers on pull
requests and pushes to `main` and `dev`, weekly, and on demand. SSH, Docker,
Kubernetes, Kafka, and Mail retain focused protocol tests until a bounded,
deterministic real-service fixture is reviewed.

## Manual Smoke

```bash
make docker-up
make docker-ps
```

Use the full stack command for rebuilds. The backend intentionally shares the
frontend container network namespace so the gateway can stay on loopback; do not
recreate only the frontend service during local testing.

Then verify:

1. The UI opens on `http://localhost:3210` or the configured localhost port.
2. An encrypted database can be created or unlocked.
3. Existing connector targets appear under `Ungrouped`; a project can be
   created, renamed, and assigned while saving a connector target.
4. SSH key creation shows an install command.
5. Existing SSH private key import stores the key without returning private material in API responses.
6. SSH config discovery or parsing can prefill an SSH connector form without silently importing private keys.
7. SSH connector connection test asks for host fingerprint approval on first contact.
8. A Postgres connector target/profile can be created with a dedicated read-only database role.
9. Postgres Console can browse schemas/tables, prepare a `SELECT ... LIMIT`
   query from the browser, and run it through the structured activity surface.
10. Postgres connector operations can create a managed scoped database role with
    a random password saved as an encrypted credential profile.
11. Postgres connector operations can download a SQL dump and restore a SQL
    dump only after typing the connector target name exactly. Restore ignores
    `psqlrc`, validates the exact artifact size, rejects psql meta-commands,
    requires proof that the complete input stream was consumed, and records
    every post-start failure or missing completion marker as
    `outcome_unknown` without automatic retry. Success, failure, cancellation,
    and uncertain outcomes all produce a required terminal audit record. A
    durable artifact-bound key prevents response loss from dispatching twice.
    Explicit transaction-control SQL is tested as an uncertain-outcome boundary,
    not advertised as an atomic restore guarantee. Malformed, repeated, mismatched,
    or shell-active `\\restrict` markers must fail before `psql` starts. A lost
    transaction response and pending terminal audit must replay the original
    operation identity without redispatch. `PREPARE TRANSACTION` is uncertain,
    while ordinary prepared statements remain accepted. Concurrent target or
    profile mutation is blocked for the full restore. History purge retains a
    non-secret replay tombstone, and tombstone expiry is independently tested.
12. A ClickHouse connector can connect directly and Over SSH through the native
    protocol with a dedicated read-only credential profile.
13. ClickHouse Console can browse visible databases, tables, and ordered
    columns, prepare bounded SQL, and persist results in structured History.
14. ClickHouse rejects writes and multi-statement SQL, caps `max_rows` at 1000,
    preserves duplicate result columns with deterministic names, caps the final
    serialized output, and returns timeout, authentication, and network failures
    without exposing the stored password.
15. A token can enable one project, hide another project, and keep separate
    target/profile/action grants inside the enabled project.
16. MCP `list_connector_targets`, `get_connector_actions`, and
    `call_connector_action` omit and reject targets from hidden projects.
17. An `approval_required` SSH, Postgres, or ClickHouse connector action appears in Console and can be Run or Declined.
18. An `always_run` SSH command streams to the persistent console, while Postgres and ClickHouse actions appear in the structured activity surface and History.
19. History and Audit Logs show and filter the project snapshot together with connector kind, target/profile context, input, output, status, and redacted errors.
20. Console can upload a queued set of local files to a remote folder, including
    overwrite confirmation when a remote file already exists. SSH overwrite
    succeeds only with atomic POSIX rename support; unsupported servers fail
    closed without removing the existing destination.
    A new destination, including `overwrite=false`, is published with the
    OpenSSH hardlink extension so a target created after the preflight check
    cannot be replaced. Servers without that atomic no-replace primitive fail
    closed instead of falling back to a racy rename.
    Restart recovery removes durably recorded remote staging artifacts through
    the owning connector, while completed download archives retain their
    persisted private-temp expiry across restart. Unresolved cleanup evidence is
    retention-safe and blocks target/profile mutations. Startup temp scavenging
    removes only old, unreferenced, regular files with owned filename patterns;
    symlinks and unknown files remain untouched. Its namespace is derived from
    the durable local database-copy identity, which rotates on import; copied
    databases that retain the same backup workspace UUID therefore cannot
    scavenge each other's staging files.
21. Console can download one or more remote files, pause/resume or cancel an
    active queue, and History can show completed transfer metadata through the
    unified connector activity stream. Multi-file downloads should save as a zip.
22. Settings can download an `.aipdb` backup and import it as a named database.
23. A Mail profile can test implicit TLS and STARTTLS without sending a message;
    IMAP-only mode keeps SMTP unavailable and separate SMTP credentials stay
    encrypted.
24. Mail folder listing preserves configured policy order, maps configured Sent,
    Archive, and Trash roles, and labels folder/message/server content as
    untrusted external data.
25. Mail unread search and message reads leave the IMAP Seen flag unchanged;
    explicit mark read/unread, move, archive, and Trash moves affect only the
    exact UID/UIDVALIDITY reference.
26. Mail compose/reply approval shows every recipient including BCC, complete
    bounded text, safe formatted projection, and a captured source reference.
    Verify rejected recipients and unknown final SMTP responses are not retried
    automatically.
27. Over-SSH Mail test and host reachability reject a missing or cross-project
    transport before network access.
28. Mail protocol tests lock context-driven connection cancellation, the total
    SMTP deadline, single-part `BODY.PEEK[TEXT]`, MIME traversal/result bounds,
    RFC recipient parsing, classified error redaction, and unknown final SMTP
    response semantics. The IMAP library parses envelope/BODYSTRUCTURE data
    inside the 4 MiB connection budget; 10-level/100-part caps apply while the
    connector traverses the parsed tree.
29. S3 Files browse a prefix with more than one page, use **Load more** without
    losing prior entries/selections, expand a bounded prefix, and reject an
    object over 512 MiB or a queue over 100 objects / 1 GiB before transfer.
30. S3 upload uses conditional single PUT or multipart completion when
    overwrite is disabled; a concurrent or repeated write cannot replace the
    object. Presigned no-overwrite upload results expose the signed
    `If-None-Match: *` required header.
31. S3 version and lifecycle dialogs distinguish versions/delete markers,
    paginate exact-key history, require destructive acknowledgement, and show
    bounded lifecycle raw XML.
32. Remote backup records show service quota/usage and retention policy in the
    same dialog. Preview changes no state; save without **Apply now** reports no
    deletion; save with it applies the preview while preserving the final
    recovery version.

## npm Publish Checks

Dry run before publishing:

```bash
cd packages/mcp
npm pack --dry-run

cd ../npm-placeholder
npm pack --dry-run
```

For public releases, prefer npm trusted publishing or provenance from CI. Local manual publish is acceptable for early testing, but it does not provide the same supply-chain signal.

Tagged container publication and MCP publication verify the exact tag commit
before publishing. The source commit must have successful CI jobs and both
CodeQL language analyses, and its tag, package versions, generated release
notes, changelog, and build metadata must agree. A rerun is evaluated by its
newest check run, so an old failed attempt cannot mask a newer result or block a
successful rerun.

Real-service connector conformance is a mandatory release check for the exact
source commit. The ClickHouse, Postgres, Valkey, RabbitMQ, and S3 job must be
successful before tagging; a temporary service outage therefore pauses the
release rather than weakening the connector contract. Native dependency
freshness remains an advisory signal because it can require a documented
maintainer review instead of an automatic dependency change. Source
verification reports stale or failed advisory signals for that review.

Container CI treats fixed HIGH and CRITICAL Trivy findings as blocking. Published
GHCR images include explicit BuildKit SBOM and maximum-mode provenance
attestations plus keyless Cosign signatures bound to immutable image digests.

The MCP package includes `server.json` plus `mcpName` metadata for MCP Registry compatibility. Keep those values aligned with `packages/mcp/package.json`.
