# Scoped Credential Resource Contracts

This standard-library-only package owns persistent resource types shared by
connector domain code and gateway adapters. Import it directly; do not recreate
the types or alias them through the gateway adapter package.

Core binds each store to one connector kind and resource class. A mutable store
can operate only within that scope. A public evidence reader exposes only `Get`;
its concrete implementation must not expose secret access, enumeration or writes.
Neither runtime supplies target resolution, console access or principal authority.

Domain journals can depend on these contracts without importing gateway runtime,
HTTP, database or Vault owners. Adapter capability providers consume the same
contracts through `gatewayconnectorapi`. The architecture guard keeps this owner
standard-library-only, while existing store, evidence and adapter tests preserve
scoping and the read-only capability boundary.
