# Managed Role Evidence

This package owns the PostgreSQL managed-role lifecycle journal. Core supplies
only a connector-scoped credential resource store; no service-specific table,
workspace access, or credential decryption is required.

The caller must hold workspace lifecycle exclusion across snapshot, persistence,
remote dispatch and confirmation. Resource store updates are not compare-and-swap.

- Persist a provisioning intent before any role mutation.
- Bind the actual role OID before the remote transaction commits. Preserve the
  exact cluster, database, role and ownership-successor identities.
- Confirm provisioning only after an acknowledged commit or exact remote
  identity verification under a tested catalog fence.
- Persist a cleanup intent before destructive dispatch. A lost acknowledgement
  or failed readback never authorizes dispatch or automatic repetition.
- Confirm cleanup only after the complete transaction is acknowledged. Role-name
  absence alone proves neither ownership reassignment nor privilege revocation.
- Confirm rollback only after remote rollback is acknowledged. Connection loss
  is not rollback evidence.

Records use canonical, bounded JSON and immutable operation identifiers. Every
transition checks the complete previous record, changes its generation, and
verifies fresh persisted readback. Unresolved records fence same-target endpoint
changes and same-cluster aliases, including admin or database changes.

This journal is evidence storage, not a remote identity verifier or a catalog
lock implementation. Its confirmation methods must not be called on the basis
of name-only observations. Wiring, remote catalog fencing and operator recovery
are separate PostgreSQL-owned lifecycle responsibilities.

## Runtime And Credential Binding

The connector's resource adapter supplies the typed `postgres_role_journal`
capability through `ScopedResourceCapabilityProvider`. The gateway supplies only
the connector/class-scoped store; no PostgreSQL-specific gateway table, runtime
switch, console adapter or secret accessor is introduced.

`Authority` hashes the complete public target/admin revision, including project,
config, timestamps and credential secret revision, without reading any secret.
Derived target refs do not change authority. `VerifyAuthority` compares that
current snapshot separately from remote identity; persisted fields cannot serve
as the current authorization source. A separate `target_digest` binds the same
public target without requiring the historical administrator to remain active.
It is not sufficient authority for any remote operation.

For a local deletion retry, the optional `ProvisionedCredentialCleanupEvidence`
contract receives only read-only `EvidenceCapabilityProvider` capabilities.
Postgres exposes `postgres_role_cleanup_evidence`, not the mutable journal.
Its concrete reader cannot decrypt, create, update or delete resource records.
Only a complete `cleaned` record matching the immutable profile reference and
current target, followed by a fresh identical readback, authorizes local
retirement without looking up or decrypting the original administrator.
Missing, malformed, stale, provisioned, rolled-back or unresolved records never
authorize that shortcut. Normal remote cleanup still requires the full current
target/admin authority and tested remote identity/catalog fencing.
The generic result boundary rejects errors and follow-up handles even when the
status says `completed`, both before and after redaction. A projection failure
or cancellation cannot retire the credential.

Credential `managed_identity` holds an immutable `Reference`: resource ID as
decimal text, full intent and exact role OID. Journal generation/status remain in
the journal, allowing a local deletion retry to observe confirmed cleanup without
editing credential metadata. `ResolveReference` rejects swapped identities.
Provisioning compensation can use this reference before any local profile ID
exists. Markerless profiles require explicit operator adoption or manual cleanup,
never an automatic name-only fallback.

`ConfirmCleanupRollback` is only for acknowledged rollback of the entire cleanup
transaction. It returns to provisioned state; `ConfirmRollback` instead records
that the original creation transaction rolled back. Neither is valid after an
unacknowledged COMMIT followed by an already-closed transaction.

## Inspection

`HistoryForTarget` validates the full journal, selects the exact local target and
returns ascending resource-ID keyset pages of at most 64 entries. A cursor never
authorizes a transition and a missing row is not remote outcome evidence. This
bounds each response, not the underlying resource-store list or its memory use.
The read-only local operator adapter does not read secrets or contact PostgreSQL.
