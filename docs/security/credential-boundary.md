# Credential Boundary

The core security rule:

> Credentials never leave the local gateway.

The AI assistant, MCP client, and API token never receive SSH private keys, SSH passwords, database passwords, or decrypted connection strings.

The preferred MVP model is Dokploy-style SSH bootstrap. aipermission does not
ask for a VPS SSH password. The gateway can generate SSH keypairs, store private
keys in the local encrypted vault, and show the user a public key install
command to paste on the server. Users may also explicitly import an existing
private key into the same encrypted local vault.

## Stored Secrets

The gateway vault may store:

- gateway-generated or explicitly imported SSH private keys
- future PostgreSQL or database credentials
- future connection secrets

The SQLite database is encrypted with SQLCipher. Secret payloads such as SSH private keys are also encrypted by the gateway vault layer. API token lookup uses hashes. Token values are shown once by default; if reusable token copy is enabled in Security, token values created after that setting is enabled are stored with vault encryption for local MCP setup.

The database password is unrecoverable. If it is lost, the local DB cannot be opened. The user must create a new DB/key/token set and manually remove old public key lines from remote servers if needed. See [Storage Encryption](storage-encryption.md).

## SSH Key Install Model

The user creates or imports an SSH key in the web UI:

- type: generated keys support `ed25519` or `rsa`; imported keys may also use
  other supported OpenSSH private key formats
- name: for example `main`, `production`, or `dev`

The gateway stores the private key in its encrypted vault. The public key is
shown to the user. If an imported key is passphrase-protected, the passphrase is
used only during import and is not saved.

Install command shape:

```txt
mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%s\n' 'ssh-ed25519 <PUBLIC_KEY> aipermission' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
```

This avoids collecting server passwords. The bootstrap action happens from the user's own terminal.

## API Tokens

An API token is not an SSH credential. It does not replace an SSH private key or database password. It is still a bearer credential for the local gateway, so anyone holding it can perform the operations allowed by that token while the relevant database is unlocked.

The MCP Started/Stopped runtime switch is an additional local safety brake.
Stopping MCP does not revoke tokens or delete saved token/server permissions; it
blocks new MCP command execution until the local user starts MCP again from the
web UI. By default, unlocked databases start with MCP execution stopped unless
Security enables automatic MCP start for that database.

Rules:

- token values are masked in the web UI
- token values are shown once by default
- reusable token copy can be enabled in Security for tokens created afterward
- tokens can be created with an expiration timestamp for temporary MCP access
- token lookup uses hashes
- stored reusable token values are encrypted by the gateway vault
- revoked or expired tokens are rejected by MCP endpoints
- web REST endpoints use a local HttpOnly browser session cookie after unlock, not token auth

Token values remain inside the SQLCipher database protected by the local unlock password.

The database password is used only to unlock or re-authenticate the selected local database. It is not sent as a bearer token on REST or MCP requests. If the browser session cookie is missing or expired while the backend process still has an unlocked database, the UI asks for the same database password again and issues a new local browser session.

Command text and command output may be stored in encrypted local history/audit records. Users should not put secret values directly into command strings and should be careful when asking AI to print files or environment variables that may contain secrets.

Basic redaction is enabled by default for common secret patterns before history, transcripts, messages, MCP response fields, and audit payloads are persisted or returned. Redaction is best-effort and can be extended with custom regex rules in Security.

The gateway is designed only for a localhost trust boundary. Docker Compose publishes host ports on `127.0.0.1`, and the backend rejects non-local remote clients plus non-localhost Host headers. The backend also refuses to start when `AIPERMISSION_BACKEND_HOST` is `0.0.0.0` or any non-loopback address.

Do not publish the Compose ports on `0.0.0.0` or a LAN address. The localhost bind is the security boundary. Docker NAT can make external clients appear as the host gateway from inside the container, which is outside the supported security model. Host-header checks are defense in depth only and do not make remote/LAN exposure safe. Remote/LAN use is unsupported.

This local-only boundary is a deliberate product boundary. AIPermission is not a shared web application, not a hosted gateway, and not a DevOps platform. The supported trust model is one developer using their own local unlocked gateway to reach their own configured SSH targets.

## MCP Boundary

MCP responses must never include:

- SSH private keys
- SSH private key passphrases
- database passwords
- decrypted connection strings
- vault encryption keys

`list_servers()` returns only metadata for servers the token may access.

`exec()` uses credentials only inside the gateway at execution time.

SSH host key pins live in the local `known_hosts` file under the data path. That file is outside the encrypted database and contains remote host key metadata only, not SSH private keys.

`known_hosts` is gateway-level trust state, not per-database secret state. If multiple named databases use the same AIPermission data directory, they share the same host key pins. This avoids repeated fingerprint approval for the same host, but users should not treat named databases as separate SSH host-trust sandboxes.

## Audit Boundary

Audit logs must not contain credential values.

Allowed fields include:

- token id or token name
- server id
- command text
- status
- exit code
- short stdout/stderr preview
- user note

Disallowed fields include:

- decrypted secret payloads
- private key bodies
- private key passphrases
- full database connection strings
