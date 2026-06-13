# Release Checklist

Run this before publishing a public release candidate.

Shortcut:

```bash
make release-check
```

## Backend

```bash
cd backend
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

## Frontend

```bash
cd frontend
npm test
npm run build
npm run test:e2e
npm audit --omit=dev --audit-level=moderate
```

## MCP Package

```bash
cd packages/mcp
npm test
npm run build
npm audit --omit=dev --audit-level=moderate
npm pack --dry-run
```

## NPM Placeholder Package

```bash
cd packages/npm-placeholder
npm pack --dry-run
```

## Supply Chain

- Confirm `CHANGELOG.md` includes the release notes for the tag being published.
- Confirm Dependabot is enabled for Go, npm, Docker, and GitHub Actions.
- Confirm CodeQL is enabled for Go and JavaScript.
- Confirm CI secret scanning passes.
- Review the non-blocking container image vulnerability scan output.
- Confirm GitHub Actions are SHA-pinned and workflow permissions are minimal.
- Confirm Docker base images are digest-pinned.
- Prefer the `Publish MCP Package` workflow with npm provenance for public releases.
- Require npm 2FA for package publishing/settings in the `aipermission` npm organization.
- Keep `@aipermission/mcp` as the real package and `aipermission` as the unscoped placeholder.
- Confirm `packages/mcp/package.json` `mcpName` matches `packages/mcp/server.json`.
- Confirm the bundled MCP operator skill matches `docs/skills/aipermission-operator/SKILL.md`.
- After the first public tag, do not edit baseline migration 1; add schema changes as migration 2+.

Manual early publish commands:

```bash
cd packages/mcp
npm publish --access public --provenance

cd ../npm-placeholder
npm publish
```

## Manual Smoke

- Start the local stack with `docker compose up -d --build`.
- Create or unlock an encrypted database.
- Capture fresh screenshots after final visual polish: unlock screen, dashboard, console approval dialog, tokens permissions, settings/security, history/audit.
- Create an SSH key and confirm the install command copies correctly.
- Add an SSH connector target and run the SSH connection test.
- Approve the first SSH host fingerprint after verifying it from a trusted source.
- Create a token and grant one connector target/action permission.
- Run one `approval_required` MCP command and approve it from Console.
- Run one `always_run` MCP command and confirm live Console output appears.
- Check the History page for the executed command and confirm the Audit Logs page shows the persisted audit entries from the encrypted database.
- Download a `.aipdb` backup and import it into a new named database.

## Security Review

- Confirm Docker ports are bound to `127.0.0.1` and no Compose override publishes the UI/API on `0.0.0.0` or a LAN interface.
- Confirm `AIPERMISSION_BACKEND_HOST=0.0.0.0` fails startup with a local-only error.
- Confirm frontend nginx returns 403 for non-local Host headers.
- Confirm browser-like mutating requests without allowed `Origin` or `Referer` are rejected.
- Confirm docs describe AIPermission as local-only, not LAN-shareable, not remotely hostable, and not a DevOps platform.
- Confirm no credentials, `.aipdb` files, `.env` files, or SSH private keys are tracked.
- Confirm command examples do not print secrets by default.

## Public Repository Polish

- Confirm README screenshots use the final theme and current UI.
- Confirm `CHANGELOG.md` is updated for the release tag.
- Confirm root docs and `docs/` agree on current endpoints, ports, local-only boundary, and package names.
- If this is the first public tag and migration history was churned during private development, collapse/shrink migrations before tagging so first users start from a clean schema baseline.
- Publish `@aipermission/mcp` with the intended version.
- Publish or republish the unscoped `aipermission` placeholder with a new version after npm's unpublish cooldown if needed.
- Create initial GitHub labels: `security review`, `mcp client`, `docs`, `screenshots`, `dogfooding`, `good first issue`, `local-only`, `question`.
