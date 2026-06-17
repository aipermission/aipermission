# Development Testing

Use the root `Makefile` for the common verification set.

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

- backend unit tests
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
3. SSH key creation shows an install command.
4. Existing SSH private key import stores the key without returning private material in API responses.
5. SSH config discovery or parsing can prefill an SSH connector form without silently importing private keys.
6. SSH connector connection test asks for host fingerprint approval on first contact.
7. A Postgres connector target/profile can be created with a dedicated read-only database role.
8. Postgres Console can browse schemas/tables, prepare a `SELECT ... LIMIT`
   query from the browser, and run it through the structured activity surface.
9. Postgres connector operations can create a managed scoped database role with
   a random password saved as an encrypted credential profile.
10. Postgres connector operations can download a SQL dump and restore a SQL
    dump only after typing the connector target name exactly.
11. A token can be scoped to one connector target/profile/action combination.
12. MCP `list_connector_targets`, `get_connector_actions`, and `call_connector_action` show only token-allowed actions for the selected target/profile.
13. An `approval_required` SSH or Postgres connector action appears in Console and can be Run or Declined.
14. An `always_run` SSH command streams to the persistent console, while a Postgres action appears in the structured activity surface and History.
14. History and Audit Logs show connector kind, target/profile context, input, output, status, and redacted errors.
15. Console can upload a queued set of local files to a remote folder, including
    overwrite confirmation when a remote file already exists.
16. Console can download one or more remote files, pause/resume or cancel an
    active queue, and History can show completed transfer metadata through the
    unified connector activity stream. Multi-file downloads should save as a zip.
17. Settings can download an `.aipdb` backup and import it as a named database.

## npm Publish Checks

Dry run before publishing:

```bash
cd packages/mcp
npm pack --dry-run

cd ../npm-placeholder
npm pack --dry-run
```

For public releases, prefer npm trusted publishing or provenance from CI. Local manual publish is acceptable for early testing, but it does not provide the same supply-chain signal.

The MCP package includes `server.json` plus `mcpName` metadata for MCP Registry compatibility. Keep those values aligned with `packages/mcp/package.json`.
