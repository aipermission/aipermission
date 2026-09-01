# Contributing

Thanks for wanting to improve `aipermission`.

The project is in active MVP testing. The current focus is a reliable local developer workflow:

- Docker Compose local runtime
- safe connector credential handling
- MCP connector action execution
- approval flow
- persistent SSH console visibility
- clear documentation

Before proposing scope changes, read [Project Principles](docs/project-principles.md).
AIPermission's maintainer and decision model is documented in
[Governance](GOVERNANCE.md).
AIPermission is local-only, single-user, developer-focused, connector-based,
and human-in-the-loop. Hosted SaaS, team RBAC, remote gateway hosting,
LAN-accessible deployments, and cloud-managed execution are intentionally out
of scope for the core project.

## Development

Backend development uses CGO and OpenSSL 3 for SQLCipher. On Debian/Ubuntu,
install `build-essential` and `libssl-dev`. On macOS, install OpenSSL 3 with
Homebrew and expose its include/library paths to CGO. Windows contributors can
use the repository Docker build, which owns these native dependencies. The
published runtime image includes `libssl3` explicitly.

Install the frontend and MCP packages from their canonical package-local
lockfiles:

```bash
npm ci --prefix frontend --workspaces=false
npm ci --prefix packages/mcp --workspaces=false
```

The package-local lockfiles are canonical. Do not create a root
`package-lock.json`; repository hygiene rejects one to prevent local/CI
dependency drift.

Run backend tests:

```bash
cd backend
go test ./...
```

Run frontend tests and build:

```bash
npm --prefix frontend exec -- playwright install chromium --with-deps
npm --prefix frontend run lint
npm --prefix frontend run format:check
npm --prefix frontend test
npm --prefix frontend run build
```

Use `npm --prefix frontend run format` to apply the repository Prettier
rules. React hook suppressions are tracked in
`frontend/eslint-suppressions.json` with an enforced budget of zero. New
warnings fail lint. Fix the code instead of adding inline suppressions; the
prune command remains available if a temporary reviewed baseline is ever
introduced.

Run Playwright when a change touches route-level UI, approval dialogs, console,
or connector template rendering:

```bash
npm --prefix frontend run test:e2e
```

Build MCP bridge:

```bash
npm --prefix packages/mcp run build
```

If your AI client runs from the repository root, use the local MCP command in
[MCP Client Setup](docs/setup/mcp-client-setup.md#local-package-development)
instead of the normal `npx -y @aipermission/mcp` command.

When adding a new target type, start with [Add A Connector](docs/development/add-a-connector.md).
New connectors must use the shared target/profile/action permission pipeline
instead of adding connector-specific approval, history, audit, or MCP tool
families.

New connector PR checklist:

- add `backend/internal/connectors/<kind>/` and register it in the built-in
  connector registry; runtime-backed built-ins also add their adapter
  side-effect import in that same registry file
- add frontend templates under `frontend/src/connectors/templates/<kind>/`;
  `metadata.json` and `index.jsx` are auto-discovered by the template registry
  and catalog
- keep secrets in credential profile schemas, not target or action schemas
- use the shared target/profile/action permission model; do not add connector
  permission, approval, history, audit, or MCP tool families
- document any intentional runtime adapter exception before using
  `RuntimeContext.Capabilities`; runtime adapters must use the shared typed
  `internal/connectorapi` contracts instead of connector-local server/runtime
  interfaces
- keep frontend template metadata valid; supported icons are documented in
  `docs/development/add-a-connector.md`, and missing required slots/model
  exports fail `npm test`
- update smoke/tests that assert the built-in connector list, backend registry,
  routes, and frontend template folder evaluation
- run `npm --prefix frontend test` so template registry modules are evaluated,
  not only string-smoked

Run the full local stack:

```bash
docker compose up -d --build
```

## Release Notes

`release-notes.json` is the canonical source for public release content. Do not
edit `CHANGELOG.md` or `frontend/src/lib/release.generated.json` directly.
After changing the canonical source, regenerate and verify both artifacts:

```bash
npm run release-notes
npm run release-notes:check
npm run version:check
```

The frontend intentionally bundles only the latest entries; the generated
`CHANGELOG.md` remains the complete public history.

`docs/connectors.md` is generated from frontend connector `metadata.json`
files. Do not edit its connector table directly. Regenerate and verify it with:

```bash
npm run connector-catalog
npm run connector-catalog:check
```

## Pull Requests

Contributions are submitted for distribution under the repository's
`AGPL-3.0-only` license. A separate CLA or DCO sign-off is not currently
required.

Before opening a PR:

- keep changes focused
- update docs when behavior changes
- avoid logging or returning credentials
- run the relevant tests/builds
- describe manual testing for MCP, approvals, or console changes

## Security Boundaries

Do not add code that exposes:

- SSH private keys
- gateway vault secret
- database passwords
- backup files
- raw credentials in logs, API responses, MCP responses, or audit payloads

When in doubt, open an issue first.
