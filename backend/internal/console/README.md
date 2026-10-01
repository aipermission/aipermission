# Console Package

`internal/console` owns persistent live connector console sessions.

Responsibilities:

- create, list, attach, resize, close, and close-by-runtime-profile console sessions
- keep live connector sessions separate from HTTP handlers
- multiplex websocket clients attached to one PTY session
- capture live output recipients with each transcript update, so newly attached
  clients receive that update through their snapshot or live output, not both
- enforce local hardening limits for websocket clients, input size, and high-frequency input/resize messages
- execute AI commands through the persistent shell
- detect long-running command state
- keep raw transcript parsing separate from cleaned display output
- redact transcript text before persistence through the injected redactor

Shutdown closes work admission, waits for admitted manual history operations,
and performs the final capture and stale manual-row cleanup before destroying
the session's exact-value redactor. A prompt recognized during a delayed insert
is completed inside that already-admitted operation, not submitted as new work
after shutdown. Canonical command and history projection updates share a
transaction; automated rows are outside manual cleanup.

Non-responsibilities:

- HTTP auth, CSRF, and route registration
- MCP token permission checks
- audit log writes
- database unlock and workspace switching

The API package should call `console.Manager` and translate returned errors into HTTP responses. This keeps the console runtime testable without a web server and gives contributors a clear place to work on terminal behavior.
