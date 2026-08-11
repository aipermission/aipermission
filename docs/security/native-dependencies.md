# Native Dependency Inventory

AIPermission tracks native code separately from ordinary Go and npm package
updates because Go vulnerability tooling may not fully model C code embedded in
a wrapper module.

The machine-readable inventory is
[`native-dependencies.json`](native-dependencies.json). CI checks that it agrees
with `backend/go.mod` and the runtime version assertion in `internal/db`.

## SQLCipher

| Component                                      | Pinned version                                | Notes                                                     |
| ---------------------------------------------- | --------------------------------------------- | --------------------------------------------------------- |
| `github.com/SE-I-T-Digital/go-sqlcipher`       | commit `8f19266d2b27`                          | Go SQLite driver containing the amalgamated native source |
| SQLCipher runtime                              | `4.16.0 community`                            | Verified with `PRAGMA cipher_version` in backend tests    |
| OpenSSL provider                               | `3.x` from the pinned Debian image repository | Exact package version is captured in the image SBOM       |
| Latest official SQLCipher reviewed for update  | `4.17.0`                                      | Not claimed as active until the wrapper embeds it         |

The backend does not rely on an untracked system SQLCipher library. The wrapper
module contains the native SQLite/SQLCipher amalgamation compiled into the Go
binary. Its OpenSSL cryptographic provider is linked from the backend image's
explicit `libssl3` runtime package. Container provenance and SBOM attestations
record the resolved native package; CI and local container builds install the
matching development headers explicitly.

The committed SQLCipher 4.4.2 fixture contains synthetic data only. Its checked
hash protects the compatibility baseline from accidental regeneration. The
active runtime must open it before an update can be accepted.

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

The scheduled `Native Dependency Freshness` workflow compares the pinned
wrapper commit and the last reviewed official SQLCipher release with GitHub. It
opens no pull request and merges nothing; a changed source fails the scheduled
check so a maintainer can review it deliberately.

Dependabot and `govulncheck` remain useful signals for the Go wrapper, but they
are not treated as complete native-code CVE coverage.
