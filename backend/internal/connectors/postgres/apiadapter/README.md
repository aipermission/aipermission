# Scoped Role Journal Adapter

This adapter implements the reviewed generic
`ScopedResourceCapabilityProvider` contract. The core supplies a resource store
already bound to this connector kind; the adapter narrows it to the managed-role
resource class and exposes the typed `rolejournal.Journal` capability.

No console runtime, credential-profile-as-runtime identity, async action port,
database handle, principal resolver or Vault access is required. A missing store
remains unusable; the domain lifecycle fails closed before opening a remote
connection. The journal caller must retain workspace-exclusive admission through
remote mutation, durable confirmation and local profile publication.

See [journal invariants](../rolejournal/README.md) and
[catalog fencing](../rolecatalog/README.md). Native encrypted-store, lost
acknowledgement and real PostgreSQL conformance tests remain required; adapter
unit tests alone do not establish remote safety.

## Local Operator Inspection

The optional `TargetOperationRunner` exposes `role-lifecycle-status` through the
existing authenticated local target-operation route. It reads only the scoped
journal; it does not dial PostgreSQL, decrypt a credential, authorize dispatch,
adopt an existing role, clear a fence, or infer success from a missing record.
The action-runtime capability remains resource-only.

The request is `{}` or `{ "after_resource_id": "64" }`. Cursors are canonical,
nonnegative decimal strings, not floating-point IDs. A response contains the
target ID, at most 64 entries, `has_more` and `next_after_resource_id`; each entry's
`resource_id` is also decimal text to avoid JavaScript precision loss. Follow-up
pages use the returned cursor. Empty results serialize as `entries: []`.
The journal validates its entire scoped resource class before filtering a page,
so corrupt or duplicate records cannot be hidden by another target or cursor.
Storage failures return a static conflict response without exposing their cause.

The response is bounded, but the scoped store currently reads all journal rows.
Pagination is not a storage-work or memory bound. Same-cluster aliases belonging
to another local target are not listed by this target's status view. Adoption,
external evidence attestation and reconciliation remain separate responsibilities;
this inspection endpoint is not a recovery implementation.
