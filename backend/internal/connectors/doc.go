// Package connectors defines the small internal contract used by
// connector-shaped targets.
//
// The package is intentionally transport-agnostic. It does not know about SSH,
// Postgres, Redis, or API recipes. Concrete connectors describe actions and
// execute approved actions; the gateway core remains responsible for token
// authentication, permission rules, approval, redaction, history, and audit.
package connectors
