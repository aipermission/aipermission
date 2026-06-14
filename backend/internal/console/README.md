# Console Package

`internal/console` owns persistent live connector console sessions.

Responsibilities:

- create, list, attach, resize, close, and close-by-runtime-profile console sessions
- keep live connector sessions separate from HTTP handlers
- multiplex websocket clients attached to one PTY session
- enforce local hardening limits for websocket clients, input size, and high-frequency input/resize messages
- execute AI commands through the persistent shell
- detect long-running command state
- keep raw transcript parsing separate from cleaned display output
- redact transcript text before persistence through the injected redactor

Non-responsibilities:

- HTTP auth, CSRF, and route registration
- MCP token permission checks
- audit log writes
- database unlock and workspace switching

The API package should call `console.Manager` and translate returned errors into HTTP responses. This keeps the console runtime testable without a web server and gives contributors a clear place to work on terminal behavior.
