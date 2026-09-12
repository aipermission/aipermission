# API Package

`internal/api` is the HTTP/MCP composition boundary. Keep this package focused on:

- wiring gateway-owned route and handler groups
- local-only HTTP boundary checks
- UI session and CSRF checks
- MCP HTTP handlers
- workspace lock/unlock orchestration
- transport DTO translation into narrow gateway-owner ports

Do not put long-running runtime loops in this package. If code owns sockets, PTYs, connector session lifecycle, or background goroutines, prefer the relevant connector/runtime package and keep API handlers thin.

The local maintenance shell is one example: `internal/maintenanceconsole` owns
its process supervisor, PTY, clients, and transcript. This package owns only
the authenticated HTTP handlers and lifecycle audit adapter. The executable
composition root injects that runtime through the console-domain port; API code
must not construct or import the concrete process implementation.

Current contributor map:

- `server.go`, `server_options.go`: process composition and one-time gateway-owner binding
- `routes.go`: gateway handler-group composition; `internal/api/httptransport/routes.go` owns the local HTTP route contract
- `http_boundary.go`, `http_security.go`, `ui_session.go`: local browser trust boundary
- `unlock_runtime.go`, `workspace_lifecycle_http_adapter.go`: encrypted database and workspace lifecycle transport
- `mcp*.go`: MCP auth, connector tool endpoints, and response shaping
- `command_request_composition.go`, `bulk_command_http_adapter.go`: live-console command transport composition
- `connector_*_adapter.go`, `*_http_adapter.go`: transport adapters for one gateway-owned workflow
- `audit.go`, `diagnostics.go`, `retention_adapter.go`: thin observation and maintenance adapters

When adding behavior, start with a small file named after the workflow. If the behavior grows beyond HTTP handling, introduce or reuse a domain package and keep the handler thin.

Handlers that still belong to this package should live on a narrowly named group such as `mcpHandlers`, not directly on `*Server`. Prefer an owning domain HTTP adapter when one exists; keep `*Server` methods for composition and shared lifecycle boundaries.

`internal/api` must satisfy the repository's shared source, package, function,
and dependency fan-out budgets without overrides. Production API files must not
re-export domain types or rebuild raw workspace scopes; architecture tests keep
those constraints fail-closed.
