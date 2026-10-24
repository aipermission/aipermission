# Native Dependency Inventory

AIPermission tracks native code separately from ordinary Go and npm package
updates because Go vulnerability tooling may not fully model C code embedded in
a wrapper module.

The machine-readable inventory is
[`native-dependencies.json`](native-dependencies.json). CI checks that it agrees
with `backend/go.mod`, `backend/go.sum`, the runtime assertions in `internal/db`,
the pinned container bases, and the explicit CGO/OpenSSL build boundary.

The same offline hygiene entry point also checks the
[dependency license metadata inventory](dependency-licenses.md). That inventory
references this native record without claiming native license compliance.

## SQLCipher

| Component                                     | Pinned version                                       | Notes                                                       |
| --------------------------------------------- | ---------------------------------------------------- | ----------------------------------------------------------- |
| `github.com/SE-I-T-Digital/go-sqlcipher`      | commit `8f19266d2b2782e348b49e04848c6830429c8174`    | Go SQLite driver containing the amalgamated native source   |
| SQLCipher runtime                             | `4.16.0 community`                                   | Verified with `PRAGMA cipher_version` in backend tests      |
| Embedded SQLite runtime                       | `3.53.1`                                             | Verified with `sqlite_version()` in backend tests           |
| OpenSSL provider                              | `3.x` from the digest-pinned Debian image repository | Exact package version is captured in the image SBOM         |
| Latest official SQLCipher reviewed for update | `4.19.0`, commit `c4b275a47932...`                   | Reviewed 2026-10-24; not active until the wrapper embeds it |

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

The 2026-10-24 review checked the release advisory, the GitHub advisory pages,
the pinned amalgamation, and the wrapper's unchanged source commit. Two known
native fixes remain absent from the embedded SQLCipher 4.16.0 runtime:

- [Malformed `hexkey` handling](https://github.com/sqlcipher/sqlcipher/commit/0c1c46a84620766483395543c66a71181fb33150)
  can create an unencrypted database instead of rejecting an invalid key.
- [Schema alias escaping in `sqlcipher_export()`](https://github.com/sqlcipher/sqlcipher/commit/4abaa9cd85d2c0e3344e6029c90a6e6806d36673)
  protects callers that supply attacker-controlled attached schema names.

AIPermission uses neither API surface: the opener encodes the filename and
`_key` passphrase separately, and snapshots use parameter-bound `VACUUM INTO ?`.
`sqlcipher_security_test.go` exercises real create/import/migration entry points
with URI-option-looking filenames and passwords, adversarial snapshot names,
encrypted headers, exact-password reopen, and rekey isolation. These tests
protect the application boundary; they do **not** patch the native library.

**Open native-update follow-up:** consume a reviewed wrapper commit embedding
these upstream fixes once available, then rerun the encrypted fixture suite.
No patched artifact is currently published on the selected wrapper branch.
Maintainers accepted this limited deferral for v0.2.73; arbitrary SQL access or
future use of either affected API requires a new assessment before release.
The inventory's `no-known-applicable-advisory` result describes current
application reachability, not absence of known native issues. Neither
`govulncheck` nor the container scan certifies the embedded C code. This release
does not change the storage runtime or database compatibility baseline.

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

## Frontend Runtime Packages

The frontend Dockerfile upgrades the pinned nginx image's Alpine packages and
requires `libx11>=1.8.14-r0` for CVE-2026-88806, alongside the existing PCRE2 and
TIFF minimums. The unprivileged base is pinned to nginx 1.30.6's immutable image
digest, preserving its matched nginx modules. It also requires
`nginx>=1.30.6-r0` for CVE-2026-15176, CVE-2026-93479 and CVE-2026-97205; see the
[upstream nginx advisories](https://nginx.org/en/security_advisories.html).
This package floor is not evidence that the application's configuration exposes
each advisory's prerequisites. A source regression preserves the guards and
the return to unprivileged UID101. Scan the final rebuilt image: package
minimums alone do not certify that all image dependencies are free of known
vulnerabilities.
Use a fresh vulnerability database for pre-push scans and retain its metadata
alongside the exact image identity. A still-valid cached database can predate
new advisories and is not evidence of parity with CI's fresh scan.

The frontend also requires `brotli-libs>=1.2.1-r0` for CVE-2026-111497. This
native decoder dependency is separate from the Go Brotli module and Node.js's
bundled decoder; patching one does not patch the others. See the
[upstream Brotli releases](https://github.com/google/brotli/releases).
The reviewed Node.js builder is 24.22.1, and MCP startup separately enforces
patched Node release lines. A loaded library or advisory match is not proof
that the application exposes the decoder's malformed-input prerequisites.

## Disposable Protocol Qualification

The conformance-only Docker VM uses Docker 28.5.1 from its immutable image
digest in `backend/testdata/connector-conformance/Dockerfile`. Alpine's kernel
and OpenSSH, and Debian's QEMU/CPIO, resolve from distribution repositories at
fixture-image build time. They are not application runtime dependencies and
are not claimed to be byte-reproducible. The guest has no NIC or host mounts;
its daemon uses only owned RAM storage. Dovecot/Postfix/OpenSSH fixtures have
the same build-time distribution-package limitation. Production SQLCipher and
container-base checks above remain unchanged.
