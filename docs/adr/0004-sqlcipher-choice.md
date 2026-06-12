# ADR 0004: SQLCipher For Local Databases

Status: accepted

## Context

AIPermission stores connector targets, credential profiles, token metadata,
audit data, command/action history, message queues, and vaulted secret payloads.
The database may be backed up and moved between machines.

## Decision

AIPermission uses SQLCipher for full local database encryption. Secret payloads
inside the database are also handled through the gateway vault layer.

## Consequences

- Database passwords are unrecoverable.
- `.aipdb` files are portable encrypted workspace backups.
- The vault layer is not a second independent security boundary if the database
  password is also compromised.
- REST and MCP responses must never return SSH private keys or database secrets.

## Related

- [Storage Encryption](../security/storage-encryption.md)
- [Credential Boundary](../security/credential-boundary.md)
