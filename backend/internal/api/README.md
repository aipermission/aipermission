# API Package

`internal/api` owns HTTP routing and request/response boundaries. Keep this package focused on:

- route registration
- local-only HTTP boundary checks
- UI session and CSRF checks
- MCP HTTP handlers
- workspace lock/unlock orchestration
- thin calls into domain packages

Do not put long-running runtime loops in this package. If code owns sockets, PTYs, connector session lifecycle, or background goroutines, prefer the relevant connector/runtime package and keep API handlers thin.

Current contributor map:

- `routes.go`: route surface, handler group wiring, and health/status handlers
- `http_boundary.go`, `http_security.go`, `ui_session.go`: local browser trust boundary
- `unlock*.go`, `databases.go`: encrypted database and workspace lifecycle
- `mcp*.go`: MCP auth, connector tool endpoints, and response shaping
- `command_request*.go`: live-console command tracking and detail queries for UI-origin console flows
- `*_handlers.go`: REST handlers for one resource family
- `messages.go`, `connector_action_approvals.go`, `audit.go`, `retention.go`: cross-cutting user workflow APIs

When adding behavior, start with a small file named after the workflow. If the behavior grows beyond HTTP handling, introduce or reuse a domain package and keep the handler thin.

Handler methods should usually live on a small handler group such as `tokenHandlers`, `mcpHandlers`, or `consoleHandlers`, not directly on `*Server`. Keep `*Server` methods for shared lifecycle, security, workspace, and cross-cutting helpers.
