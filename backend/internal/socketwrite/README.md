# Console Socket Writes

This package owns serialized, deadline-bounded WebSocket JSON and control
writes shared by live connector and maintenance consoles. Session owners keep
authorization, message content, redaction, client membership, and lifecycle.

- `JSON` acquires an optional client writer mutex.
- `JSONLocked` requires existing exclusive writer ownership, including the
  snapshot registration fence.
- `Control` serializes control frames under the same optional writer mutex.
- `KeepAlive` uses its owner's positive ping interval. A failed control write
  closes the socket to wake the owner reader; normal stop preserves it.

The two-second timeout applies to each socket write after ownership is
acquired. It does not bound mutex acquisition or an entire sequential
broadcast. After a write failure, callers must remove/close the failed client
instead of retrying a corrupted WebSocket writer.

Tests exercise real Gorilla framing over owned HTTP/net.Pipe fixtures. The
console owner also has a mandatory loopback TCP regression with bounded
socket buffers and a healthy reader alongside a nonreading, Pong-sending
client. Port-free tests cannot prove operating-system TCP behavior.
