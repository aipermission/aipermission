# SSH Key Model

The preferred SSH model is Dokploy-style key bootstrap.

aipermission does not collect VPS SSH passwords. The gateway can generate SSH
keypairs or explicitly import an existing private key. In both cases, private
keys are stored in the local encrypted vault and the public key/install command
is shown to the user.

## Key Types

Generated key types:

- `ed25519`
- `rsa`

The recommended default is `ed25519`.

Imported keys support common OpenSSH private key formats that the backend can
parse, including ed25519, rsa, and ecdsa keys. Imported RSA keys must be at
least 2048 bits. Passphrase-protected imports use the passphrase only during
import; the passphrase is not saved.

## User Flow

1. The user opens the SSH Keys page.
2. The user creates an `ed25519` or `rsa` key, or imports an existing private key.
3. The gateway stores the private key in the encrypted vault.
4. The gateway shows the public key and install command.
5. The user runs the install command on the VPS from their own terminal.
6. The user selects that SSH key when creating a server record.
7. The gateway opens SSH connections with that private key.
8. On the first connection, the gateway returns the remote host key fingerprint for explicit approval.
9. After approval, the remote host key is stored in the gateway `known_hosts` file.

The Servers page can also import SSH host entries to prefill server forms.
This imports host metadata only; it does not silently read or import private key
material referenced by `IdentityFile`. Wildcard-only blocks such as `Host *` are
used only as defaults for concrete hosts and are not shown as standalone server
entries. `ProxyCommand` is reported as configured, but the raw command is not
returned. In Docker, gateway config scan reads the container user's config;
choose a config file or paste config content to parse a local workstation config.

## Install Command

The gateway shows a command in this shape:

```txt
mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%s\n' 'ssh-ed25519 <PUBLIC_KEY> aipermission' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
```

The command appends the public key and does not overwrite the existing `authorized_keys` file.

## Server Uninstall

The Servers uninstall dialog offers:

- delete the local record only
- remove remote `authorized_keys` entries containing the selected gateway public key blob, then delete the local record

Remote cleanup matches the public key blob, so it can remove entries even if the authorized_keys comment or options were changed. If cleanup fails or removes zero entries, the local server record is kept so the user does not lose track of a possible remote leftover.

## Security Boundary

Responses may show:

- public key
- fingerprint
- install command
- key name
- key type

Responses must not show:

- private key
- vault encryption secret
- decrypted secret payloads

A server record does not contain credentials. It references the gateway key by `ssh_key_id`.

Some SSH targets, especially NAS appliances, show an interactive menu before a
normal shell. Server records can store optional advanced startup behavior for
that compatibility case: startup input sent after connect, or a forced shell
command. These values are not credentials. Keep them empty for normal Linux
servers and avoid putting secrets in them because console transcripts and audit
metadata can include shell output.

## Host Key Verification

Gateway SSH clients use explicit first-connect fingerprint approval:

- The first unknown host key returns `unknown_ssh_host_key` with the host, key type, SHA256 fingerprint, and public host key payload.
- The user should verify the fingerprint through a trusted channel such as the VPS provider console or their own trusted terminal.
- After approval, the host key is stored under the local data path `known_hosts` file. This file is outside the encrypted database and stores host key pins only, not SSH private keys.
- Later connections for the same host/port verify the host key.
- If the host key changes, the connection is rejected.

If the server was intentionally rebuilt or rotated, the user must deliberately remove the related `known_hosts` entry.

This reduces MITM risk without breaking the passwordless SSH key workflow. Approval is still a trust decision: if the first fingerprint is approved without verifying it elsewhere, a first-connection MITM can still be trusted by mistake.
