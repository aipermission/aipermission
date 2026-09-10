// Package actions owns the generic connector action lifecycle.
//
// It resolves target/profile references, prepares connector-owned actions,
// enforces authorization immediately before dispatch, persists redacted
// requests and results, and coordinates approval, recovery, and idempotency.
// HTTP and gateway composition stay behind injected runtime ports.
package actions
