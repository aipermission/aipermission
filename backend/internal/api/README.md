# API Package

`internal/api` is the HTTP/MCP composition boundary. Keep this package focused on:

- wiring gateway-owned route and handler groups
- local-only HTTP boundary checks
- UI session and CSRF checks
- MCP HTTP handlers
- workspace lock/unlock orchestration
- thin calls into domain packages

Do not put long-running runtime loops in this package. If code owns sockets, PTYs, connector session lifecycle, or background goroutines, prefer the relevant connector/runtime package and keep API handlers thin.

The local maintenance shell is one example: `internal/maintenanceconsole` owns
its process supervisor, PTY, clients, and transcript. This package owns only
the authenticated HTTP handlers and lifecycle audit adapter. The executable
composition root injects that runtime through the console-domain port; API code
must not construct or import the concrete process implementation.

Current contributor map:

- `routes.go`: gateway handler-group composition; `internal/gatewayinfrastructure/routes.go` owns the route table and accepts the health/status handlers
- `http_boundary.go`, `http_security.go`, `ui_session.go`: local browser trust boundary
- `unlock*.go`, `databases.go`: encrypted database and workspace lifecycle
- `mcp*.go`: MCP auth, connector tool endpoints, and response shaping
- `command_request*.go`: live-console command tracking and detail queries for UI-origin console flows
- `*_handlers.go`: REST handlers for one resource family
- `messages.go`, `connector_action_approvals.go`, `audit.go`, `retention.go`: cross-cutting user workflow APIs

When adding behavior, start with a small file named after the workflow. If the behavior grows beyond HTTP handling, introduce or reuse a domain package and keep the handler thin.

Handlers that still belong to this package should live on a narrowly named group such as `mcpHandlers`, not directly on `*Server`. Prefer an owning domain HTTP adapter when one exists; keep `*Server` methods for composition and shared lifecycle boundaries.
