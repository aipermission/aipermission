# Security Policy

`aipermission` is a local developer tool that can execute commands on servers you configure. Treat it as sensitive software.

## Supported Versions

The project is pre-1.0 and in active MVP testing. Security fixes are handled on the main branch until the first public release process is defined.

## Reporting A Vulnerability

Please do not open a public issue for vulnerabilities that could expose credentials or enable unintended command execution.

For now, report privately through GitHub Security Advisories on the repository, or contact the repository owner through GitHub if advisories are unavailable.

Expected response targets:

- acknowledgement within 48 hours
- initial triage within 7 days
- coordinated disclosure target within 90 days for confirmed issues, unless the reporter and maintainers agree otherwise
- urgent fixes may be released sooner when credential exposure or unintended command execution is involved

Include:

- affected version or commit
- clear reproduction steps
- impact
- whether credentials, tokens, or command execution are involved

## Security Boundaries

### Local-Only Design Decision

AIPermission is a local developer gateway, not a remote service and not a DevOps platform.

The supported deployment model is:

```txt
developer machine -> local Docker gateway -> SSH to configured remote servers
```

The unsupported deployment models are:

```txt
LAN users -> shared gateway
internet users -> public gateway
team members -> central hosted gateway
```

Remote servers may be managed through SSH, but the AIPermission gateway itself must stay on localhost. After database unlock, the web REST API requires a local HttpOnly browser session cookie. That cookie is a localhost UX/session guard, not a remote multi-user authentication model.

Current MVP boundaries:

- SSH private keys stay in the local gateway.
- MCP clients authenticate with API tokens.
- Web REST calls authenticate with the local browser session cookie issued after database unlock.
- Tokens are scoped to explicit server permissions.
- Revoked tokens are rejected by MCP endpoints.
- The SQLite database is encrypted with SQLCipher and must be unlocked after backend startup.
- The database password is only used to unlock or re-authenticate the encrypted local database. It is not used as a bearer token for REST or MCP requests.
- The database password can be changed only while the current password is known; it is not recoverable.
- The database password is escaped before it is passed to SQLCipher PRAGMA key/rekey handling. Regression tests cover quotes and semicolons so user-entered password text cannot change SQL parsing.
- Regression tests assert SQLCipher is active, the configured cipher page size is applied, KDF iterations are non-zero, and SQLite foreign keys are enabled on encrypted connections. `govulncheck` and Dependabot watch dependency risk; keep the SQLCipher driver on the newest available v4 module release.
- Secret fields are encrypted with the gateway vault secret inside the encrypted database. The vault derives its AES-GCM key with HKDF-SHA256; the HKDF salt is public domain-separation data, not a second secret.
- `AIPERMISSION_GATEWAY_SECRET` can be omitted for automatic generation. If it is set explicitly, it must be at least 32 characters. The visible local-development placeholder is replaced by a generated high-entropy secret at startup.
- API tokens are shown once by default. If reusable token copy is enabled in Security, newly created token values are stored with gateway vault encryption for local MCP setup. Disabling the setting clears stored reusable token values.
- API token authentication stores and compares SHA256 hashes of high-entropy random tokens. This is not password hashing; low-entropy user passwords are handled by SQLCipher instead.
- SSH host keys are verified with a local `known_hosts` file outside the encrypted database. It contains host key pins only, not SSH private keys. The first connection returns a SHA256 fingerprint that must be approved in the UI before the key is recorded; later key mismatches are rejected.
- Default Docker port publishing is localhost-only. The backend rejects non-local remote clients plus non-localhost HTTP Host headers; remote/LAN mode is intentionally unsupported.
- The frontend nginx layer also rejects non-local Host headers before serving UI assets or proxying `/api`.
- Browser-origin mutation checks reject cross-site state-changing requests, require a valid UI session cookie after unlock, and require the UI CSRF header for protected REST mutations. Browser-like mutation requests without an allowed `Origin` or `Referer` are rejected.
- The backend refuses to start if `AIPERMISSION_BACKEND_HOST` is `0.0.0.0` or any non-loopback bind address. Use `127.0.0.1`.
- Do not change Compose port bindings from `127.0.0.1:...` to `0.0.0.0:...` or a LAN address. The localhost bind is the security boundary. Docker NAT can make external traffic appear as the host gateway from inside the container, which is outside the supported security boundary. Host-header checks are defense in depth only and do not make remote/LAN exposure safe.

Known risks:

- AIPermission runs the exact command text through a shell on the target server. Shell operators such as `;`, `&&`, pipes, redirects, command substitution, and globs are interpreted by that shell. This is intentional command execution, not an injection bug, but approval means approving the shell-interpreted command body.
- Command text, command output, approval notes, console transcripts, messages, and audit records may be stored in the encrypted local database and can persist secrets if the AI is instructed to read sensitive files or environment values. Basic redaction is enabled by default for common token/password/API-key/private-key patterns, and users can add custom regex rules in Security. Redaction is best-effort and cannot guarantee detection of every secret format. Approval requests keep a separate encrypted raw command payload for execution so redaction never mutates the command that is run; UI, history, messages, MCP response fields, and audit display fields remain redacted.
- Pending command approvals store an approval-context snapshot. If server profile, SSH key fingerprint, token permission, token validity, MCP tool metadata, or command payload hash changes before Run, the request becomes `stale` and must be requested again.
- The unlocked backend process is part of the trust boundary. Keep the default Docker bind on localhost; do not expose the gateway on LAN or the public internet.
- Database passwords are necessarily present in backend process memory while an unlock, import, or password-change request is being handled. The password is not stored as a bearer token or written to audit logs, but process memory should be treated as trusted while the gateway is running.
- UI sessions and auth rate-limit counters are in-memory process state. Restarting the backend clears them. Auth rate limiting is local hardening friction, not a remote security boundary. This matches the local single-user design and is not a remote account/session system.
- A malicious browser extension is treated as local machine compromise, not as a supported remote-web threat. HttpOnly cookies, SameSite cookies, CSRF checks, CORS, Host checks, and CSP reduce normal web-page and cross-site request risk, but an installed extension with broad host/page permissions may read visible UI data, observe user actions, inject UI actions, or make privileged extension-origin requests to localhost. Use a trusted browser/profile for AIPermission and avoid running untrusted extensions while the gateway is unlocked.
- UI session and CSRF cookies use `Secure`, `SameSite=Strict`, and local-only browser/session checks. The supported gateway URL remains local HTTP on `localhost`; HTTPS reverse-proxy/LAN deployment is unsupported and should not be used to reinterpret these cookies as remote auth.
- The gateway vault secret protects encrypted SSH key and reusable token payloads inside the SQLCipher database. If that secret is lost, those payloads cannot be decrypted; if it is exposed together with the unlocked database contents, vault-protected payloads should be treated as compromised.
- `always_run` should be used only for trusted, temporary maintenance flows.
- The frontend CSP is intentionally compatible with the current Vite/React build and nginx deployment; future hardening can remove any remaining inline-style allowances when the UI build supports it cleanly.

Expected CodeQL notes:

- SSH command execution is expected. AIPermission intentionally sends approved
  command text to the target server shell after token permission checks,
  approval policy checks, audit logging, and local user approval when required.
- SQLCipher `PRAGMA rekey` does not support parameter binding through the
  current driver. The rekey path escapes double quotes before constructing the
  SQLCipher passphrase literal, and regression tests cover quotes, semicolons,
  and SQL-looking password text.
