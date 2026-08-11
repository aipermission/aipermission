# Development Testing

Use the root `Makefile` for the common verification set.

Direct backend tests require a C compiler and OpenSSL 3 development headers
because the pinned SQLCipher wrapper uses CGO. Debian/Ubuntu contributors can
install `build-essential libssl-dev`; the backend Docker build installs the
same native dependency itself. CI uses the shared
`.github/actions/setup-backend-native` action so all Go jobs stay aligned.

## Quick Checks

```bash
make test
make build
make audit
```

## Release Candidate Checks

```bash
make release-check
```

This runs:

- repository secret, line-ending, source-size, and frontend hook-debt budgets
- release-version and native-dependency inventory consistency checks
- generated OpenAPI route inventory drift
- backend unit tests with a visible aggregate coverage summary
- backend race tests
- backend vet
- backend govulncheck
- frontend tests
- frontend production build
- frontend Playwright browser smoke for unlock, security settings, database import, settings retention, and token permission flows
- frontend production npm audit
- MCP package tests
- MCP package build
- MCP production npm audit
- MCP package dry pack
- unscoped placeholder package dry pack

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
    dump only after typing the connector target name exactly.
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
    overwrite confirmation when a remote file already exists.
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

Container CI treats fixed HIGH and CRITICAL Trivy findings as blocking. Published
GHCR images include explicit BuildKit SBOM and maximum-mode provenance
attestations plus keyless Cosign signatures bound to immutable image digests.

The MCP package includes `server.json` plus `mcpName` metadata for MCP Registry compatibility. Keep those values aligned with `packages/mcp/package.json`.
