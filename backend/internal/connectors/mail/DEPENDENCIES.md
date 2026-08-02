# Mail Connector Dependency Decision

The first Mail connector release pins small, protocol-focused Go libraries:

- `github.com/emersion/go-imap` v1.2.1 for stable IMAP4rev1 framing, partial
  `BODY.PEEK`, UID search/store/move, STARTTLS, and modified UTF-7 handling.
- `github.com/emersion/go-smtp` v0.24.0 and `go-sasl` for explicit SMTP
  envelope control, STARTTLS, PLAIN authentication with LOGIN fallback after
  verified TLS, and final DATA response classification. The frozen
  standard-library `net/smtp` package is not used.
- `github.com/emersion/go-message` v0.18.2 for MIME and RFC message handling.
- `github.com/microcosm-cc/bluemonday` v1.0.27 for an outbound HTML allowlist.
- `golang.org/x/net/html` and `idna` for parsed HTML-to-text projection and
  recipient-domain normalization.

These packages use permissive licenses compatible with AIPermission's AGPL-3.0
distribution. Production protocol debug writers stay disabled. All reads,
decodes, result serialization, and command lifetimes remain bounded by the
connector rather than relying on library defaults.

`go-imap` v1 is used deliberately for this release because it is the latest
stable v1 API and the connector depends on its established client behavior.
The actively developed v2 line remains pre-stable. Maintainers must review the
upstream v1 branch and security advisories during dependency maintenance, apply
relevant post-tag fixes through a reviewed upgrade when necessary, and keep
`govulncheck` in the release gate. Migration to v2 should happen only after a
stable v2 release exists and focused tests cover capability refresh, UID search,
BODY.PEEK, STARTTLS, mutation, and cancellation behavior against the new API.

The release candidate was dogfooded against a real Dovecot-compatible IMAP
service and authenticated SMTP submission service. Connection tests, folder
ordering, unread-preserving reads, compose/reply, Prompt, and Always paths were
exercised. Real-provider dogfood complements the deterministic protocol and
limit tests; it does not replace them or prove final SMTP delivery.
