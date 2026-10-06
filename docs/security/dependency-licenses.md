# Dependency License Metadata Inventory

[`dependency-licenses.json`](dependency-licenses.json) records dependency
identities, declared npm licenses, and available offline Go license-file
evidence. It is a reviewed metadata baseline, not a legal compliance decision,
license classifier, or assertion of SPDX compatibility. Independent maintainer
review is required before integrating the initial capture.

## Sources and Artifact Identity

The inventory covers the canonical frontend, MCP, and repository-tooling npm
lockfiles, their package manifests, `backend/go.mod`, `backend/go.sum`, and
[`native-dependencies.json`](native-dependencies.json). Exact SHA-256 source
hashes deliberately make even formatting changes require a baseline review.
No installed npm dependency tree is used.

The MCP SDK's transitive `proxy-addr` dependency is locked to 2.0.8 for
[GHSA-jqcg-44mw-7w3h](https://github.com/advisories/GHSA-jqcg-44mw-7w3h).
Its declared MIT license, registry integrity and lockfile metadata are reviewed
in the inventory. A behavioral regression checks the actual dependency resolved
through SDK/Express against IPv4-mapped IPv6 trust subnet spoofing. This does
not imply that the stdio MCP bridge exposes an Express HTTP server.

Each npm record retains its lockfile and installation location, package name
(including explicit alias targets), version, resolved URL, integrity, and
verbatim declared license. A canonical metadata hash covers all package-entry
fields, including development, optional, platform, and install-script flags.
Nested duplicate versions stay separate. Root package records are included.
Declarations such as `SEE LICENSE IN ...` are preserved, not interpreted or
resolved against downloaded packages.

Go requirements are parsed by the Go tool's `go mod edit -json`, not a custom
go.mod parser. The complete parser output is captured alongside its source
hash. The three-column go.sum records are validated and grouped by exact
module/version, retaining module and go.mod checksums separately. Direct and
indirect requirements are distinguished from checksum-only versions.
Replacements, excludes, and retracts currently fail generation and checking;
their addition requires an explicit extension of the inventory model.

Go evidence is collected from the supplied extracted module cache using Go's
case-escaped module/version paths. Recursive named LICENSE/LICENCE, COPYING,
NOTICE, and PATENTS documents are recorded with cache-relative paths, byte
sizes, and SHA-256 hashes. Filename discovery is intentionally limited; it is
not text-based license detection. Empty files do not count, and symlinks are
rejected. Nested evidence is retained without classifying it.

The native record references the existing native inventory and its source
hash. It does not label SQLCipher, SQLite, OpenSSL, or container packages with
invented license identifiers.

## Offline Generation and Review

Generate a candidate separately from the default read-only check:

```bash
node scripts/dependency-license-generate.js \
  --go-mod-cache "$(go env GOMODCACHE)" \
  --output /tmp/dependency-licenses.candidate.json
node scripts/dependency-license-check.js
node scripts/native-dependency-check.js
```

Use an existing cache on other machines. Generation requires a local Go binary;
`--go-binary` selects one explicitly. It sets `GOPROXY=off`, `GOSUMDB=off`,
`GOTOOLCHAIN=local`, and `GOWORK=off`, and never runs graph resolution,
downloads, npm installation, or registry queries. Without `--output`, it emits
JSON to stdout. Both CLIs accept `--root` for disposable fixtures.

Generation is deterministic for identical inputs, Go parser output, and cache
evidence; there are no timestamps or absolute cache paths. Use the reviewed
contributor Go 1.26.6 toolchain for byte-for-byte regeneration because parser
JSON fields can vary between Go versions. Every newly generated candidate has
`review.status=pending-owner-review`, so copying generated output alone cannot
pass the gate. Compare the candidate with the baseline, review changed
identities and declarations and inspect evidence files, then explicitly record
`metadata-reviewed` and the actual reviewer. Every evidence gap must retain its
exact identity and reason, a meaningful note, and the disposition
`acknowledged-owner-review-required`. This acknowledges a gap, not a license.
Commit the reviewed inventory with its input changes after maintainer review.

The checker reads repository source files and the recorded inventory only:
it needs Node but neither Go nor installed dependencies nor a module cache.
Added, changed, deleted, missing, malformed, or unsupported source metadata
fails closed. Missing or pending review, missing records, and malformed
evidence also fail. The existing hygiene command invokes this check through
`scripts/native-dependency-check.js`; verification recipes and budgets are
unchanged.

## Evidence Gaps and Limits

The local sanitizer update changes only the frontend root metadata and
DOMPurify identity to 3.4.16; its `(MPL-2.0 OR Apache-2.0)` declaration is
unchanged. All Go records, evidence, and 38 acknowledged gaps are preserved.
This remains a metadata review, not a compatibility or legal determination.

The local 0.2.64 release assembly refresh aligns the npm records and source
hashes with the current exact lockfiles: MCP SDK 1.31.0, frontend Node types
24.19.0, TypeScript ESLint parser and related packages 8.71.0, jscpd and its
platform packages 5.3.3, and undici-types 7.24.6. All recorded license
declarations are unchanged. The native inventory reference already matches
the PCRE2 runtime minimum of `10.42-1+deb12u1` enforced by the backend Dockerfile;
its hash is unchanged. The prior Go parser capture, evidence records, metadata
review, and 38 acknowledged gaps are preserved unchanged. This is an offline
metadata alignment, not a new Go evidence review or legal determination.

The 0.2.65 security refresh raises the backend PCRE2 runtime minimum to
`10.42-1+deb12u2` and updates the native inventory reference hashes. The
patched image must pass the same vulnerability scan as every release image;
the package-version guard is not a substitute for scanning the final image.

The initial offline capture has 602 npm records (including three roots),
88 Go module/version records, and local named-file evidence for all 37 current
Go requirements. The 38 absent versions are historical checksum-only entries.
They are listed individually with owner-review notes in
`review.missing_go_evidence` and in their Go evidence records:

- `github.com/davecgh/go-spew@v1.1.0`
- `github.com/emersion/go-message@v0.15.0`
- `github.com/emersion/go-sasl@v0.0.0-20200509203442-7bfe0ed36a21`
- `github.com/emersion/go-textwrapper@v0.0.0-20200911093747-65d896831594`
- `github.com/stretchr/objx@v0.1.0`
- `github.com/stretchr/testify@v1.7.0`
- `github.com/yuin/goldmark@v1.4.13`
- `golang.org/x/crypto@v0.0.0-20190308221718-c2843e01d9a2`
- `golang.org/x/crypto@v0.0.0-20210921155107-089bfa567519`
- `golang.org/x/mod@v0.6.0-dev.0.20220419223038-86c51ed26bb4`
- `golang.org/x/mod@v0.8.0`
- `golang.org/x/net@v0.0.0-20190620200207-3b0461eec859`
- `golang.org/x/net@v0.0.0-20210226172049-e18ecbb05110`
- `golang.org/x/net@v0.0.0-20220722155237-a158d28d115b`
- `golang.org/x/net@v0.6.0`
- `golang.org/x/sync@v0.0.0-20190423024810-112230192c58`
- `golang.org/x/sync@v0.0.0-20220722155255-886fb9371eb4`
- `golang.org/x/sys@v0.0.0-20190215142949-d0b11bdaac8a`
- `golang.org/x/sys@v0.0.0-20201119102817-f84b799fce68`
- `golang.org/x/sys@v0.0.0-20210615035016-665e8c7367d1`
- `golang.org/x/sys@v0.0.0-20220520151302-bc2c85ada10a`
- `golang.org/x/sys@v0.0.0-20220722155257-8c9f86f7a55f`
- `golang.org/x/sys@v0.5.0`
- `golang.org/x/term@v0.0.0-20201126162022-7de9c90e9dd1`
- `golang.org/x/term@v0.0.0-20210927222741-03fcf44c2211`
- `golang.org/x/term@v0.5.0`
- `golang.org/x/text@v0.3.0`
- `golang.org/x/text@v0.3.3`
- `golang.org/x/text@v0.3.6`
- `golang.org/x/text@v0.3.7`
- `golang.org/x/text@v0.7.0`
- `golang.org/x/tools@v0.0.0-20180917221912-90fa682c2a6e`
- `golang.org/x/tools@v0.0.0-20191119224855-298f0cb1881e`
- `golang.org/x/tools@v0.1.12`
- `golang.org/x/tools@v0.6.0`
- `golang.org/x/xerrors@v0.0.0-20190717185122-a985d3407aa7`
- `gopkg.in/check.v1@v0.0.0-20161208181325-20d25e280405`
- `gopkg.in/yaml.v3@v3.0.0-20200313102051-9f266ea9e77c`

Do not reuse another version's license to fill these gaps. Owner follow-up
must supply evidence for the exact artifact or document the unresolved gap;
this offline topic does not fetch missing sources.

go.sum is not a build graph or a distribution bill of materials. Cache files
are local observations and are not authenticated against the module h1
checksum by this tool. The default checker validates recorded evidence shape
and identity but cannot rehash files that are not present on CI. It does not
discover files outside the named-file convention or decide which nested
license terms apply to shipped code.

Standard-library, OS-package, image-SBOM, and full downloaded npm license texts
remain outside this inventory. SPDX parsing, compatibility, notice delivery,
and distribution obligations require separate owner/legal review. This is not
a history-ratcheted or cryptographically signed approval system: deliberate
baseline edits remain subject to independent review.

## Embedded Editor Sanitizer

Monaco 0.57.0 includes an embedded DOMPurify copy as well as a declared npm
dependency. An npm override alone does not replace that embedded code.
The frontend pins DOMPurify 3.4.16 and its Vite bridge redirects only Monaco's
exact sanitizer import to that package in both the production graph and Vite's
development dependency optimizer. An actual optimizer regression verifies the
patched output and source identity. Production graph verification rejects
the embedded module or a missing replacement. The override also keeps Monaco's
declared dependency on the same reviewed version.

[GHSA-p98j-92pf-mc4p](https://github.com/advisories/GHSA-p98j-92pf-mc4p)
requires `IN_PLACE` sanitization with a node-removing afterSanitize hook. The
dependency regression reproduces detached-handler retention on 3.4.15 and
checks both patched hook paths; it is not evidence that the application's
string-based editor sanitization exposed that exploit. Retain the bridge until
a reviewed Monaco upgrade removes the affected embedded copy, and rerun editor
browser tests when changing either sanitizer or editor versions.
