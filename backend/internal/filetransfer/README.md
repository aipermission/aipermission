# File Transfer Persistence

This owner stores transfer metadata, queue state, idempotency and transactional
History projections. It does not authorize connector actions or read credentials.
Protocol execution stays in connector-owned adapters.

## MCP Upload Staging

- `UploadIdentity` binds the target, action and immutable upload manifest.
  Manifests contain basename, exact destination, byte count, SHA-256 and overwrite,
  never contents or the MCP client's local source path.
- Build `MCPUploadClaim` using the authenticated token ID, not a body field.
  Its persisted token/key scope is separate from UI transfer idempotency.
- The ingress must stream bytes into a server-generated private transfer staging
  file, enforce the size cap, and verify the actual size/digest before creation.
  Neither a caller-supplied temporary path nor an unverified digest is authority.
- `CreateMCPUpload` atomically owns a staged path, enforces count/byte quotas,
  records expiry and creates its History projection. Every staged path has one
  owner; duplicate request identities replay without taking ownership of a new file.
- `GetMCPUpload` verifies token ownership and the complete identity before
  returning internal staging metadata. This is not connector execution permission:
  dispatch must still pass the ordinary connector approval and drift checks.
- The existing workspace cleanup worker expires unstarted uploads after fifteen
  minutes and removes their private staging. Running transfers are not expired.
  Cancellation, lock/restart recovery and terminal cleanup use the existing worker
  lifecycle; do not add an independent global filesystem sweeper.

Contents are not stored in SQLCipher or returned to the model. Size limits and
idempotency do not make an upload read-only: delivering a file can disclose data
and may replace a remote file when overwrite is explicitly authorized.
