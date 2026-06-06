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
5. SSH config discovery or parsing can prefill a server form without silently importing private keys.
6. Server connection test asks for host fingerprint approval on first contact.
7. A token can be created and scoped to one server.
8. An `approval_required` MCP command appears in Console and can be Run or Declined.
9. An `always_run` MCP command streams to the persistent console.
10. History and Audit Logs show the command lifecycle.
11. Console can upload a queued set of local files to a remote folder, including
    overwrite confirmation when a remote file already exists.
12. Console can download one or more remote files, pause/resume or cancel an
    active queue, and History > File Transfer History can show completed
    transfer metadata. Multi-file downloads should save as a zip.
13. Settings can download an `.aipdb` backup and import it as a named database.

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
