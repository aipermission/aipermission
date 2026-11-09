# Console Package

`internal/console` owns persistent live connector console sessions.

Responsibilities:

- create, list, attach, resize, close, and close-by-runtime-profile console sessions
- keep live connector sessions separate from HTTP handlers
- multiplex websocket clients attached to one PTY session
- capture live output recipients with each transcript update, so newly attached
  clients receive that update through their snapshot or live output, not both
- serialize output appends through live delivery, so concurrently produced
  manual output and AI display headers follow the same order as the transcript
- enforce local hardening limits for websocket clients, input size, and high-frequency input/resize messages
- execute AI commands through the persistent shell
- detect long-running command state
- expose bounded console capacity snapshots and conditionally close reviewed idle sessions
- keep raw transcript parsing separate from cleaned display output
- redact transcript text before persistence through the injected redactor
- keep independent stdout/stderr UTF-8 carry before text policy and JSON framing

`streamoutput` owns exact-byte redaction and incremental text emission; the
shared `terminaltext.UTF8Stream` decoder is also used by the maintenance PTY.
Each stream retains at most three unfinished UTF-8 bytes. Exact secret matches
and prefixes are redacted before transcoding, so decoding cannot bypass byte
redaction. On EOF, remaining invalid bytes each become U+FFFD, and the carry is
cleared. Normal output closure flushes both streams before the exit frame;
canceled or failed persistence cannot be reported as complete output observation.

Shutdown closes work admission, waits for admitted manual history operations,
and performs the final capture and stale manual-row cleanup before destroying
the session's exact-value redactor. A prompt recognized during a delayed insert
is completed inside that already-admitted operation, not submitted as new work
after shutdown. Canonical command and history projection updates share a
transaction; automated rows are outside manual cleanup.

Snapshot, live output, and error-response writes use the shared
`internal/socketwrite` boundary, also used by the maintenance console. Each
socket write has a two-second deadline after acquiring writer ownership. The
snapshot fence still places the snapshot before subsequent live frames.
Failed output writes evict that client; failed attach/error writes return so
attach cleanup can unregister it. Failed keepalive writes close the socket to
wake its reader, while normal keepalive shutdown does not close it.

This is not a two-second end-to-end broadcast guarantee: waiting for a writer
mutex and sequential delivery to multiple clients can add latency. Real TCP
backpressure tests complement port-free HTTP/Gorilla pipe fixtures; neither is
a replacement for native persistence and full-stack release checks.

Non-responsibilities:

- HTTP auth, CSRF, and route registration
- MCP token permission checks
- audit log writes
- database unlock and workspace switching

The API package should call `console.Manager` and translate returned errors into HTTP responses. This keeps the console runtime testable without a web server and gives contributors a clear place to work on terminal behavior.

## Capacity And Operator Cleanup

`capacity` owns transcript-free DTOs and the shared finite session limit. The
manager counts every retained session, including closing/finalization failures;
persisted historical rows are not capacity use. One coalescing change signal wakes
visible browser watches on session lifecycle and command state changes. A dedicated
WebSocket emits revision-only invalidations; its authenticated upgrade releases the
workspace read lease, so an idle watch never delays Lock/Switch. Each short snapshot
GET performs one bounded metadata query rather than loading transcripts. Terminal
output refreshes in-memory activity without a notification for every chunk. Browser
watches pause when hidden, coalesce events, and reconcile at 30-second intervals.

The local operator reviews exact session IDs, generations and activity timestamps
before closing. The manager rechecks these under lifecycle/input/session locks,
rejects running, unfinished and untracked input, and claims closing before releasing
the check. Bulk cleanup uses a finite idle threshold; zero threshold is limited to
explicit single-session close. New activity or replacement causes a skip, not a
forced close. Existing authorization and finalization fences remain in effect.
Ambiguous compound input remains protected even after an intermediate prompt;
the cleanup view does not infer whole-batch completion from terminal appearance.
Any manually entered terminal input keeps that session protected from Capacity
and automatic idle cleanup until it is explicitly ended. Manual History can
recognize prompts for best-effort output capture, but process output can imitate
a prompt, and a later automation attempt cannot certify prior manual work exited.
Normal explicit session close/restart remains available outside idle cleanup.

Websocket viewers are shown separately: the application also attaches background
transcript observers, so viewer presence alone is not proof of active work. Idle
means no observed I/O for the selected period, not proof that remote detached
processes are absent. Cleanup requires explicit confirmation, warns about this
boundary, audits results, and distinguishes confirmed closures from uncertain ones.

`idlecleanup` owns the opt-in encrypted policy and hourly worker. The workspace
composition supplies its local-operator identity and required audit port; it
reuses conditional cleanup rather than bypassing the execution guards. Policy
updates are atomic with audit, and serialize with the hourly pass. Defaults are
disabled/60 minutes. A single timer wakes at the next hour, not per session;
disabled policy does not scan sessions. Manager shutdown cancels and drains the
worker before database disposal. This closes transports and preserves history.
Connector-owned startup provenance crosses the neutral runtime ports so the
core can protect untracked startup work without understanding connector kinds.
An interrupt request alone never proves a command exited for cleanup purposes.
