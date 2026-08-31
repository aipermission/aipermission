# AIPermission Backup

AIPermission Backup is a separate self-hosted service that stores immutable
SQLCipher-encrypted `.aipdb` snapshots. It is a passive backup store, not a
remote AIPermission gateway, account system, team service, or control plane.

The recommended deployment is one service shared by the owner's trusted
computers over a private local network. Put an HTTPS reverse proxy in front of
the service and keep its raw port private. If clients connect from another
network, route them through a VPN or private overlay network and retain HTTPS
inside that network. Direct public-internet exposure of the backup port is not
recommended.

The service receives:

- the already encrypted `.aipdb` bytes;
- a service-specific bearer token;
- bounded metadata such as database display name, installation id, timestamp,
  byte size, and SHA-256 checksum.

It never receives the database password, gateway vault key, decrypted database
content, MCP tokens, connector credentials, SSH keys, or permission rules.

## Run The Service

Clone the separate
[`aipermission/backup`](https://github.com/aipermission/backup) repository on a
machine you control. Generate a high-entropy token and start its Compose stack:

```bash
mkdir -p secrets
openssl rand -hex 32 > secrets/backup-token
chmod 640 secrets/backup-token
printf 'AIPERMISSION_BACKUP_SECRET_GID=%s\n' "$(id -g)" > .env
chmod 600 .env
docker compose -f docker-compose.release.yml pull
docker compose -f docker-compose.release.yml up -d
```

The example stack binds to `127.0.0.1:8080`. Keep that raw port private. When
the AIPermission gateway is on another trusted machine, publish the service
through an HTTPS reverse proxy. Across different networks, use a VPN or private
overlay network instead of exposing the raw service port. AIPermission rejects
plaintext HTTP except on loopback.

The gateway backup client connects only to the provider URL saved by the local
operator. It ignores ambient `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`
settings and refuses redirects so the encrypted snapshot and provider token are
not silently routed through a process-wide proxy.

Read the token without placing it in shared shell history:

```bash
cat secrets/backup-token
```

## Add A Provider

1. Unlock the local AIPermission database.
2. Open **Settings** and find **Remote backup providers**.
3. Select **Add provider**.
4. Enter the service base URL and service token.
5. Save the provider. New providers remain disabled.
6. Select **Test** to verify authentication and protocol compatibility.
7. Select **Enable**, then enter the current database password.

Enabling remote backup applies a stronger password policy than normal local
database creation: at least 18 characters, uppercase and lowercase letters,
numbers, sufficient character diversity, and no common, repeated, sequential,
product-related, or database-name-derived patterns. This reduces risk because
possession of an encrypted backup enables offline password guessing.

Password verification happens locally. The password is not sent to the backup
service. A provider can be disabled later without deleting its remote files.

## Upload And Restore

**Upload** creates a temporary consistent SQLCipher snapshot and transfers it
unchanged. Every remote version is immutable. **Backups** lists versions stored
for the current database stream and lets the user download an encrypted file,
restore it under an editable local database name, or explicitly prune versions
older than the newest retained count. Individual rows and checkbox selections
can also delete exact historical versions. The service refuses any prune or
delete operation that would remove the final recovery version in a stream.
Restore re-lists the selected version, checks size and SHA-256 metadata,
validates the SQLCipher password and schema, and never overwrites the currently
open database.

On a new machine with no local database:

1. Open the AIPermission setup screen.
2. Select **Restore Remote**.
3. Enter the service URL and token.
4. Select the database stream and immutable backup version. Streams with the
   same display name remain distinguishable by their shortened stream id;
   versions are grouped by source installation and show relative and exact
   timestamps.
5. Choose the new local database name.
6. Enter that backup's database password and restore it.

The first-run URL, token, and password are transient request values. They are
not placed in local storage, URL parameters, or a provider record. After the
database is restored and unlocked, add a provider normally in Settings if this
machine should create future backups.

## Storage Quota And Automatic Retention

The **Backups** dialog shows service-reported storage usage, quota, remaining
capacity, backup count, stream count, and pending remote deletions. Automatic
retention is scoped to the current encrypted database stream and keeps the
newest 1–1000 immutable versions.

Preview calculates the exact version and byte counts that would be retained or
deleted without changing remote state. Saving with **Apply now** deletes the
previewed older versions immediately; saving without it only affects future
uploads. When enabled, the backup service applies the policy after a successful
immutable upload. Both automatic and explicit cleanup preserve the final
recovery version. Pending deletion bytes may continue to count toward provider
storage until the remote blob worker finishes cleanup.

Retention and quota controls require backup service protocol v2 and its
`/v1/...` storage and retention routes. A 404 or protocol error from those
controls means the separate backup service must be upgraded before the feature
can be used.

## Operational Notes

- Backups made before a database password change still require the old
  password.
- SHA-256 detects accidental corruption. It does not prove that a malicious
  storage operator has not replaced both a blob and its metadata.
- Upload, download, and restore are explicit local user actions. There is no
  background sync in this release. An enabled retention policy may run on the
  backup service immediately after an explicit successful upload.
- After unlock, AIPermission checks active providers once. If the latest remote
  version is newer than the version last uploaded or restored by this encrypted
  local database, the UI displays a stale-local-copy warning. Listing remote
  metadata never advances that baseline.
- Each database stores a stable random workspace UUID inside its encrypted
  database. That UUID is the remote stream identity, so unrelated databases
  named `Default` do not share versions. Restoring a backup preserves its UUID
  because it remains the same logical database lineage.
- Prune is an explicit destructive action scoped to one stream. It always keeps
  at least the newest version and requires confirmation in the local UI.
- Automatic retention is also stream-scoped and destructive. Preview it before
  enabling or applying it; changing the keep-latest count never authorizes
  deletion of the final recovery version. When storage is at its configured
  quota, a successful explicit upload may temporarily use the bytes that the
  same stream's retention policy will release; the metadata commit then keeps
  the final retained set within quota.
- Selected-version cleanup is also explicit, stream-scoped, limited to 100
  unique versions per request, and confirmed in the local UI. Deleting the
  final remaining version is rejected.
- The service token grants access to encrypted backup blobs. Keep it outside
  source control and rotate it if exposed.
