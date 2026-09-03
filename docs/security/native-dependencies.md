# Native Dependency Inventory

AIPermission tracks native code separately from ordinary Go and npm package
updates because Go vulnerability tooling may not fully model C code embedded in
a wrapper module.

The machine-readable inventory is
[`native-dependencies.json`](native-dependencies.json). CI checks that it agrees
with `backend/go.mod`, `backend/go.sum`, the runtime assertions in `internal/db`,
the pinned container bases, and the explicit CGO/OpenSSL build boundary.

## SQLCipher

| Component                                     | Pinned version                                       | Notes                                                       |
| --------------------------------------------- | ---------------------------------------------------- | ----------------------------------------------------------- |
| `github.com/SE-I-T-Digital/go-sqlcipher`      | commit `8f19266d2b2782e348b49e04848c6830429c8174`    | Go SQLite driver containing the amalgamated native source   |
| SQLCipher runtime                             | `4.16.0 community`                                   | Verified with `PRAGMA cipher_version` in backend tests      |
| Embedded SQLite runtime                       | `3.53.1`                                             | Verified with `sqlite_version()` in backend tests           |
| OpenSSL provider                              | `3.x` from the digest-pinned Debian image repository | Exact package version is captured in the image SBOM         |
| Latest official SQLCipher reviewed for update | `4.18.0`, commit `63697beb0faf...`                   | Reviewed 2026-08-26; not active until the wrapper embeds it |

The backend does not rely on an untracked system SQLCipher library. The wrapper
module contains the native SQLite/SQLCipher amalgamation compiled into the Go
binary. Its OpenSSL cryptographic provider is linked from the backend image's
explicit `libssl3` runtime package. Container provenance and SBOM attestations
record the resolved native package; CI and local container builds install the
matching development headers explicitly. `CGO_ENABLED=1` is explicit in the
builder image so a build cannot silently switch to a different storage runtime.

The source boundary is deliberately narrow: AIPermission consumes the
`SE-I-T-Digital/go-sqlcipher` `main` branch only through the pinned Go
pseudo-version and full commit above. It does not fetch SQLCipher C sources at
build time and does not substitute a system SQLCipher library. The module and
`go.mod` checksums in the JSON inventory must match `backend/go.sum`.

The committed SQLCipher 4.4.2 fixture contains synthetic data only. Its checked
hash protects the compatibility baseline from accidental regeneration. The
active runtime must open it before an update can be accepted.

The 4.18.0 upstream review checked the GitHub security advisory pages for both
the wrapper and official SQLCipher and found no known advisory applicable to
the pin. This is a point-in-time maintainer review, not a claim that
`govulncheck` covers the embedded C code. The release moves to SQLite 3.53.4,
avoids one Windows crash under non-default
logging with `cipher_memory_security`, and fixes an optimized GCC relocation
error. The pinned Go wrapper has not advanced and still embeds SQLCipher
4.16.0, so this review does not change AIPermission's active runtime or database
compatibility baseline.

## Update Policy

For every SQLCipher wrapper or native runtime update:

1. Review upstream wrapper and SQLCipher security advisories, not only
   `govulncheck` output.
2. Update `backend/go.mod`, the machine-readable inventory, and the expected
   runtime version together.
3. Run encrypted database create, reopen, rekey, snapshot, import, and migration
   tests against representative `.aipdb` fixtures.
4. Confirm `PRAGMA cipher_version`, the exact cipher page size, KDF iteration
   count, HMAC/KDF algorithms, and foreign keys in backend tests.
5. Document any encryption-format or compatibility change in the release notes
   and backup/restore documentation.
6. Update the full wrapper and upstream commit identities, Go checksums,
   embedded SQLite version, advisory result, and review date in the inventory.

The review must be refreshed at least every 45 days even when neither upstream
source has moved. A stale review fails the scheduled check so the absence of an
upstream version bump cannot be mistaken for a current advisory assessment.

The scheduled `Native Dependency Freshness` workflow compares the full pinned
wrapper commit, configured source branch, last reviewed official SQLCipher
release, and review age with GitHub. It opens no pull request and merges
nothing; a changed source or stale review fails the scheduled check so a
maintainer can review it deliberately.

Dependabot and `govulncheck` remain useful signals for the Go wrapper, but they
are not treated as complete native-code CVE coverage.
