# Storage Encryption

aipermission uses two storage protection layers:

1. Full SQLite database encryption with SQLCipher.
2. Field-level secret payload encryption through the gateway vault.

Project Vault values use both layers. Each value is encrypted with associated
data bound to the workspace, item id, and value revision so ciphertext cannot be
silently moved to a different item or revision. Metadata lists never decrypt
values; explicit local reveal and approved session application are the only
decryption paths.

Other persistent gateway secrets use a versioned AES-256-GCM record envelope.
Its authenticated associated data binds the ciphertext to the workspace,
secret domain, database record id, and encrypted field. Connector credentials,
connector credential resources such as SSH private keys, reusable API token
values, prepared connector actions, encrypted command bodies, and backup
provider credentials therefore fail authentication if ciphertext is copied to
another record, field, domain, or workspace.

When an existing supported database is unlocked for the first time after this
format is introduced, the gateway verifies current envelopes and rewrites
legacy field ciphertext in one database transaction. The completion marker is
committed only after every encrypted record succeeds. A malformed record,
unknown envelope version, authentication failure, or write failure rolls back
the complete rewrite and fails unlock instead of leaving a partly migrated
secret store. Records are traversed by increasing primary key and only one
ciphertext is held for migration at a time, so unlock memory use is bounded by
the largest supported record rather than total encrypted history size. Current
execution paths accept only the versioned record format; legacy decoding is
restricted to this explicit migration boundary.

## Current Model

The SQLite file is encrypted with SQLCipher 4.16.0 through a pinned Go wrapper
and the OpenSSL 3 provider. The backend does not open it automatically at
startup; the web UI first checks unlock status. A committed database generated
by the previous 4.4.2 runtime verifies that existing SQLCipher 4 files remain
readable before a native runtime update is accepted.

The database password is escaped before it is passed to SQLCipher PRAGMA key/rekey handling. Regression tests cover quotes and semicolons so user-entered password text cannot change SQL parsing.

Backend tests also assert the inventoried SQLCipher runtime is active, the
configured 4096-byte cipher page size and 256,000 PBKDF2-HMAC-SHA512 iterations
are applied, HMAC-SHA512 page authentication is active, and SQLite foreign keys
are enabled on encrypted connections. Dependency updates are
watched with `govulncheck` and Dependabot, while the embedded native runtime is
tracked under the [native dependency inventory](native-dependencies.md).

Schema migrations never rewrite the only database copy. Before opening a
supported older schema, the gateway creates an encrypted sibling file named
`<database>.pre-migration-v<version>.aipdb` with owner-only permissions. It uses
the same database password and remains available if the migration fails.
Repeated attempts from the same schema atomically replace this one recovery
slot instead of retaining an unbounded series of full database copies. A
completed pending snapshot replaces the previous slot only after creation
succeeds. To recover, stop the gateway, preserve the failed database, rename
the pre-migration snapshot to a normal `.aipdb` filename, and open it with a
gateway version that supports the snapshot schema. A successful upgrade does
not delete this rollback copy automatically; remove it only after the new
database has been verified or retain it as an encrypted recovery artifact.

Databases created with non-default SQLCipher 4 parameters are not guessed open:
the same symptom can mean a wrong password, corruption, or custom cipher/KDF
settings. Open such a file with the exact legacy runtime and parameters that
created it, export or normalize it to the documented SQLCipher 4 defaults, then
import the normalized encrypted copy. Preserve the original file first. The
gateway intentionally has no UI switch that weakens or probes cipher settings.

If no database exists, the first screen shows:

- Create Database
- Import Database

Create Database asks the user to set a local database password:

- password and confirmation are collected in two inputs
- new passwords must be at least 14 characters and include uppercase letters, lowercase letters, and numbers
- the password is never returned in backend responses
- the password is not an API token or a separate backup password
- the password is not stored as persistent plaintext
- the password cannot be recovered if forgotten

If databases exist, the login screen asks the user to choose a database first. This allows separate encrypted local databases for different projects.

For an encrypted database, the tabs are:

- Unlock Database
- New Database
- Import Database

Unlock Database requires the selected database password. New Database creates a separate named encrypted database and does not delete or archive the current one. Import Database imports a user-selected SQLCipher-encrypted `.aipdb` or `.db` file as a named local database.

Plain SQLite files are intentionally unsupported. If a plaintext SQLite file is detected in the data directory or uploaded during import, the backend rejects it instead of converting it or keeping plaintext backup files.

Until unlock succeeds, server, token, key, console, history, and MCP endpoints are unavailable.

Unlock is process-level state. Closing the browser tab does not lock the backend. This allows MCP/AI work to continue while the browser is closed.

Web REST calls use a local HttpOnly browser session cookie after unlock. If that cookie is missing or expired while the backend process still has the database open, the UI returns to the unlock form and asks for the same database password to issue a new cookie. The database password is not used as an API bearer token.

The backend can keep multiple named databases unlocked as live workspaces in the same process. The UI `Switch` action does not ask for a password when the target database is already unlocked, and it does not close the previous workspace. MCP commands, console sessions, and request polling in other unlocked workspaces continue running.

If the target database is not unlocked yet, the Switch dialog asks for that database password, opens the workspace, and makes it the active UI context.

The database password is required again when:

- the backend/container restarts
- the target named database has not been unlocked in this process
- the browser session cookie is missing or expired
- the user explicitly locks the current database or all databases

If more than one database is unlocked, the UI asks whether to lock the current database or all databases. Lock current closes only the active workspace and promotes another unlocked workspace if available. Lock all closes every unlocked workspace and stops MCP/API commands until a database is unlocked again. Switch is not a lock operation and must not interrupt running AI work.

## Change Password

Settings rekeys the active database with SQLCipher `rekey`.

- the current password must be verified first
- the new password must be at least 14 characters and include uppercase letters, lowercase letters, and numbers
- the operation applies only to the active unlocked database
- the current backend process remains unlocked
- after container restart or Lock Database, the new password is required

Previously downloaded `.aipdb` backups keep the password they had when created. Backups downloaded after Change Password use the new password.

## Delete Database

Settings can delete a named local database. Deletion is intentionally a two-step
operation:

- the UI shows the selected database name for review
- the user must enter the current database password
- the backend verifies that password against the selected SQLCipher database
  before deleting the local file

This is local destructive cleanup only. It does not connect to connector
targets, remove remote SSH `authorized_keys` lines, or revoke credentials
outside the local database file.

## Forgotten Password

If the database password is forgotten, the encrypted SQLite file cannot be opened. This is the expected security behavior and does not create a vulnerability.

The user loses:

- local connector targets and credential profiles
- API tokens
- command/session history
- gateway-stored SSH private keys
- message queue records

Unified history, audit logs, closed console sessions, and consumed messages can
also be cleaned by configurable retention settings. History retention covers
connector action requests, live-console command rows, file transfer metadata,
and the normalized history projection. Connector action data can include
bounded fetched mail bodies and encrypted outbound approval payloads; those
values also travel with encrypted `.aipdb` backups until retention cleanup.
Retention values are stored inside the encrypted database; `0` disables
automatic cleanup for that category.

Connector targets are not changed. For SSH targets, existing public key lines
may remain in remote `authorized_keys` files; the user can remove them manually
or create and install a new aipermission key.

## Backup Relationship

The active backup model is a `.aipdb` file:

- the file is a SQLCipher encrypted SQLite database
- there is no separate backup password
- import requires the database password
- the backup opens with the database password used at download time

The gateway vault derives its AES-GCM key from the gateway secret with HKDF-SHA256. The HKDF salt is public domain-separation data, not a second secret. The gateway vault secret is also stored in encrypted DB settings, so a single `.aipdb` file is enough to restore encrypted connector credentials on another machine. Record-bound associated data is derived from database identity and contains no additional secret.

The same `.aipdb` relationship applies to Project Vault values. Restoring the
encrypted database restores their ciphertext, metadata, revisions, and the
gateway material needed to decrypt them after the correct database password is
provided.

Important: the gateway vault secret is sensitive. If it is lost, vault-encrypted payloads cannot be decrypted. If it is exposed together with database contents while the database password is known, vault-protected SSH key and reusable token payloads should be treated as compromised.

`AIPERMISSION_GATEWAY_SECRET` may be omitted. In that default mode, the visible local-development placeholder is replaced at startup by a generated high-entropy secret stored in the local data directory with owner-only permissions.

The generated `gateway.secret` file is replaced atomically and kept at owner-only permissions. Startup fails closed when an existing file is empty, too short, a symlink, or not a regular file; it never replaces an invalid existing secret with newly generated material. Existing custom secrets of at least 32 characters remain supported so upgrades do not rotate gateway encryption material.

SSH host key pins are not part of the SQLCipher database. They live in the local `known_hosts` file under the data path and contain host key metadata only, not SSH private keys. A `.aipdb` backup restores gateway credentials and settings, but host key trust still belongs to the local machine that approved it.

## Product Decision

Token values are shown once by default. Tokens may also have an `expires_at`
timestamp for temporary MCP access. Token action permission grants can also have
an `expires_at` timestamp for temporary maintenance windows. When reusable token
copy is disabled, newly created token values are not stored for later copying;
authentication still uses SHA256 hashes of high-entropy random token values.

If the user enables reusable token copy for local convenience, token values created after that point are stored in encrypted `token_value` form through the gateway vault and can be copied again from the UI. Disabling the setting clears stored reusable token values. Token hashes remain for authentication, but token values cannot be recovered after clearing.
