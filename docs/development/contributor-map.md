# Contributor Map

Use this page to find the owning boundary, then read its canonical contract.
It is a navigator, not another implementation of security rules.

## Ownership

| Change                                                    | Start Here                                                                         | Contract                                                  |
| --------------------------------------------------------- | ---------------------------------------------------------------------------------- | --------------------------------------------------------- |
| Connector schemas, preparation and external execution     | `backend/internal/connectors/<kind>`                                               | [Add a Connector](add-a-connector.md)                     |
| Connector UI and local request ownership                  | `frontend/src/connectors/templates/<kind>` and `_shared`                           | [Development Architecture](architecture.md)               |
| Permission, approval and public result projection         | `backend/internal/gatewayaccess`, `gatewayconnectoractions`, `gatewayconnectorapi` | [Permission Flow](../architecture/mcp-permission-flow.md) |
| Workspace unlock, retirement and bounded shutdown         | `backend/internal/workspacelifecycle`, `workspaceruntime`                          | [Local Gateway](../architecture/local-gateway.md)         |
| Credential resolution, cleanup and redaction              | `backend/internal/connectorcredentials`, `connectorcapabilities`                   | [Credential Boundary](../security/credential-boundary.md) |
| Vault grants, requests and exact-session authorization    | `backend/internal/vaultactions`, `vaultrequests`, `vaultsessions`                  | [Vault API](../api/mcp-tools.md#project-vault-tools)      |
| Durable request/operation state                           | `backend/internal/connectortargets`, `commandrequests`, `gatewayoperations`        | [REST API](../api/rest-api.md)                            |
| MCP setup, private config writers and response validation | `packages/mcp/src`                                                                 | [MCP Client Setup](../setup/mcp-client-setup.md)          |
| Test discovery, immutable baselines and release gates     | `scripts`, `maintenance-policy.json`, frontend/MCP policies                        | [Testing](testing.md)                                     |

Connector-specific transports and templates do not own a second approval,
audit, history or credential pipeline. Browser completions must still belong
to the current draft, dialog, session and request generation; matching an ID
alone is not enough after switching away and back.

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
