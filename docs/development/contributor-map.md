# Contributor Map

Use this page to find the owning boundary, then read its canonical contract.
It is a navigator, not another implementation of security rules.

## Ownership

| Change                                                          | Start Here                                                                         | Contract                                                  |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------------- | --------------------------------------------------------- |
| Connector schemas, preparation and external execution           | `backend/internal/connectors/<kind>`                                               | [Add a Connector](add-a-connector.md)                     |
| Connector UI and local request ownership                        | `frontend/src/connectors/templates/<kind>` and `_shared`                           | [Development Architecture](architecture.md)               |
| Permission, approval and public result projection               | `backend/internal/gatewayaccess`, `gatewayconnectoractions`, `gatewayconnectorapi` | [Permission Flow](../architecture/mcp-permission-flow.md) |
| Workspace unlock, retirement and bounded shutdown               | `backend/internal/workspacelifecycle`, `workspaceruntime`                          | [Local Gateway](../architecture/local-gateway.md)         |
| Credential resolution, cleanup and redaction                    | `backend/internal/connectorcredentials`, `connectorcapabilities`                   | [Credential Boundary](../security/credential-boundary.md) |
| Vault grants, requests and exact-session authorization          | `backend/internal/vaultactions`, `vaultrequests`, `vaultsessions`                  | [Vault API](../api/mcp-tools.md#project-vault-tools)      |
| Durable request/operation state                                 | `backend/internal/connectortargets`, `commandrequests`, `gatewayoperations`        | [REST API](../api/rest-api.md)                            |
| Released baseline definitions and transactional upgrades        | `backend/internal/db/baselineschema` (definitions), `db` (migration policy/I/O)    | [Development Architecture](architecture.md)               |
| Backup-service reflected credential rejection                   | `backend/internal/backups/serviceboundary`                                         | [Credential Boundary](../security/credential-boundary.md) |
| Submitted terminal text and best-effort tracking classification | `backend/internal/console/manualinput` (text), `console` (session/history)         | [Development Architecture](architecture.md)               |
| MCP setup, private config writers and response validation       | `packages/mcp/src`                                                                 | [MCP Client Setup](../setup/mcp-client-setup.md)          |
| Test discovery, immutable baselines and release gates           | `scripts`, `maintenance-policy.json`, frontend/MCP policies                        | [Testing](testing.md)                                     |

Connector-specific transports and templates do not own a second approval,
audit, history or credential pipeline. Browser completions must still belong
to the current draft, dialog, session and request generation; matching an ID
alone is not enough after switching away and back.

Released baseline SQL belongs to `internal/db/baselineschema`; it returns
caller-owned ordered statement slices, never a shared mutable plan. Add schema
changes as new numbered migrations in `internal/db`, not by rewriting released
baseline definitions. The DB owner still controls upgrade ordering, preflight
checks, transactions, projection synchronization and restart recovery. The
native baseline contract tests pin both SQL order and the resulting SQLite
schema; run the complete DB suite when changing either owner.

Backup-service metadata and binary downloads share the mandatory
`internal/backups/serviceboundary` owner. It rejects reflected credential
representations and holds stream suffixes until cross-chunk validation is
possible. A stream failure is terminal; never flush or retry held data after
credential rejection or a destination error. Zero-value boundaries fail closed.
Remote protocol calls, checksums, temporary-file cleanup, installation and
audited provider mutations remain in their respective service/workflow owners.

`internal/console/manualinput` owns bounded command previews, escape/input
capture and best-effort interactive classification. A capture is session-local;
its caller must serialize input and must not advance it after a failed terminal
write. Text classification is not shell parsing, an authorization decision or
proof of execution. Console owns execution exclusion, prompt/stream positions,
session closure, mandatory redaction and transactional history projection.
Run both `go test ./internal/console/manualinput` and the complete console suite
when changing this boundary; do not duplicate classification in connectors.

Human credential testing, backup and provisioning are composed by
`backend/internal/connectormanagement`. Reuse its credential operation runtime
preparation after lifecycle admission; do not repeat decrypt, boundary and
runtime construction in HTTP handlers. Cancellation during preparation must
prevent connector dispatch. Provisioning retains its original secret boundary
for bounded post-dispatch compensation, which must not inherit cancellation.

Shell polling, request-generation invalidation and gateway context composition
live in `frontend/src/components/use-app-shell-controller.ts`; the Shell owns
rendering. Preserve settlement-based scheduling and route-specific refresh
behavior when changing this controller.

Token-page issuance, revocation, permission refresh and expiry filtering live
in `frontend/src/components/tokens/use-token-page-controller.ts`; the page owns
presentation. Its protected page tests exercise the actual controller through
the rendered UI rather than a second copy of token lifecycle logic.

## Results And Recovery

| Observation                                                                        | Safe Next Step                                                                                                                                  |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `approval_pending` or `running`                                                    | Follow `assistant_hint` and poll the original request; do not submit a duplicate action.                                                        |
| `output_withheld`                                                                  | Authorization no longer permits projecting content. Do not recover it through another UI/log path.                                              |
| `gateway_response_contract_invalid`                                                | A read response failed validation. Inspect the contract mismatch; this is not proof that a mutation failed.                                     |
| `gateway_transport_outcome_unknown` or `gateway_response_contract_outcome_unknown` | The mutation may have reached the gateway. Keep the original key and unchanged input; reconcile explicitly, never invent a new attempt blindly. |
| Terminal gateway `outcome_unknown`                                                 | Inspect recorded and external state before considering another attempt; respect `retry_policy`.                                                 |
| `stale`, expired authorization or retired workspace                                | Obtain fresh valid context. Do not bypass authorization or resurrect a previous lease.                                                          |

The [MCP contract](../api/mcp-tools.md) distinguishes bridge
uncertainty from a terminal gateway outcome. Domain code must not infer failure
from an interrupted transport or successful commit from a missing error.

## Native And Browser Checks

Use the exact Go and Node versions in [CONTRIBUTING](../../CONTRIBUTING.md).
CGO is required: the database wrapper links against OpenSSL 3. The reviewed
image digests and native dependency limitations are recorded in
[Native Dependencies](../security/native-dependencies.md).

For contributors without a compatible host toolchain, the backend Docker
builder owns Go, CGO and OpenSSL. From the repository root:

```bash
docker build --target build -t aipermission-backend-dev ./backend
docker run --rm --mount type=bind,src="$PWD",dst=/repository,readonly \
  --workdir /repository/backend aipermission-backend-dev go test ./...
```

This is a native unit-test entry point, not the complete release gate. Initial
image construction needs registry/package access. Browser tests require the
package-local locked dependencies and Playwright Chromium. Follow
[Testing](testing.md) for race, coverage, real-service, platform and security
checks; a cross-build is not native Windows/macOS execution evidence.

Use an isolated Compose project, loopback port and named volume for experiments.
Do not recreate an operator's active gateway or reuse its database for tests.
Rebuild the complete frontend/backend pair when handing over a Docker change;
a frontend-only recreation can leave the backend in the previous network
namespace. Never use existing production targets for mutation smoke tests.
