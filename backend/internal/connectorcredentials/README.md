# Credential Runtime Ownership

This shared owner builds public credential snapshots, secret accessors and
credential result boundaries. Protocol-specific ownership verification and
remote cleanup remain in the connector. `RuntimePorts` is composed by core;
it is not passed to connector implementations.

## Completed Cleanup Evidence

A connector may implement `ProvisionedCredentialCleanupEvidence` when it keeps
durable proof that an external credential cleanup already completed. This is
an optional local-retirement preflight, not a general recovery or execution API.

The caller must hold exclusive workspace lifecycle admission before loading
the current target/profile and retain it through evidence verification, local
retirement, transactional audit and lifecycle invalidation. The evidence
context contains only the public target and connector-owned read-only
capabilities. It has no principal, secret accessor or execution transport.

An adapter supplies those capabilities through `EvidenceCapabilityProvider`.
`EvidenceResourceRuntime` exposes concrete Get-only resource readers, not a
mutable store hidden behind a smaller interface. Normal resource/action
providers are never called. Network, command and session-environment
capabilities are rejected by both reserved name and actual interface, so
renaming one cannot grant execution authority.

- A nil result with no error falls back to normal authenticated cleanup.
- An error stops retirement; it never authorizes the fallback.
- A non-nil result must be `completed`, without an error or any follow-up handle.
  These requirements are checked before and after shared result projection.
- Missing, stale, malformed or unresolved connector evidence must not be
  interpreted as completion. Remote name absence is not completion evidence.
- Cancellation, resource-factory failure or projection failure stops retirement.
  The preflight never decrypts or looks up a historical administrator.

Normal remote cleanup still receives its separately authenticated runtime and
must enforce the connector's current public authority and remote identity
checks. Read-only evidence cannot authorize a remote mutation. Public snapshot
maps isolate top-level keys; nested metadata follows the existing immutable
snapshot contract rather than granting a mutable database view.
