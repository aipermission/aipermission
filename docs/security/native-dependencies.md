# Native Dependency Inventory

AIPermission tracks native code separately from ordinary Go and npm package
updates because Go vulnerability tooling may not fully model C code embedded in
a wrapper module.

The machine-readable inventory is
[`native-dependencies.json`](native-dependencies.json). CI checks that it agrees
with `backend/go.mod` and the runtime version assertion in `internal/db`.

## SQLCipher

| Component                             | Pinned version    | Notes                                                     |
| ------------------------------------- | ----------------- | --------------------------------------------------------- |
| `github.com/mutecomm/go-sqlcipher/v4` | `v4.4.2`          | Go SQLite driver containing the amalgamated native source |
| SQLCipher runtime                     | `4.4.2 community` | Verified with `PRAGMA cipher_version` in backend tests    |

The backend does not rely on an untracked system SQLCipher library. The wrapper
module contains the native SQLite/SQLCipher amalgamation compiled into the Go
binary.

## Update Policy

For every SQLCipher wrapper or native runtime update:

1. Review upstream wrapper and SQLCipher security advisories, not only
   `govulncheck` output.
2. Update `backend/go.mod`, the machine-readable inventory, and the expected
   runtime version together.
3. Run encrypted database create, reopen, rekey, snapshot, import, and migration
   tests against representative `.aipdb` fixtures.
4. Confirm `PRAGMA cipher_version`, cipher page size, KDF iterations, and foreign
   keys in backend tests.
5. Document any encryption-format or compatibility change in the release notes
   and backup/restore documentation.

Dependabot and `govulncheck` remain useful signals for the Go wrapper, but they
are not treated as complete native-code CVE coverage.
