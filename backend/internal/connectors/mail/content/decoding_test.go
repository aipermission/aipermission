package mailcontent

import (
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/emersion/go-imap"
)

type failedBodyReader struct{ err error }

func (r failedBodyReader) Read([]byte) (int, error) { return 0, r.err }

func TestTextDecodingEnforcesEncodingAndUTF8Budgets(t *testing.T) {
	cases := []struct {
		name, input, encoding, charset, subtype, want string
		limit, decoded                                int
		truncated, complete, wantError                bool
	}{
		{name: "plain", input: "hello", limit: 10, want: "hello", decoded: 5, complete: true},
		{name: "base64", input: "aGVsbG8=", encoding: "BASE64", limit: 10, want: "hello", decoded: 5, complete: true},
		{name: "quoted_printable", input: "caf=C3=A9", encoding: "quoted-printable", limit: 10, want: "caf\u00e9", decoded: 5, complete: true},
		{name: "latin1", input: "caf\xe9", charset: "iso-8859-1", limit: 10, want: "caf\u00e9", decoded: 5, complete: true},
		{name: "html", input: "<p>hello</p>", subtype: "html", limit: 20, want: "hello", decoded: 12, complete: true},
		{name: "bounded_plain", input: "abcdef", limit: 3, want: "abc", decoded: 4, truncated: true},
		{name: "bounded_unicode", input: "\u20ac\u20ac", limit: 4, want: "\u20ac", decoded: 5, truncated: true},
		{name: "unsupported_encoding", input: "hello", encoding: "x-unknown", limit: 10, wantError: true},
		{name: "unsupported_charset", input: "hello", charset: "x-unknown", limit: 10, wantError: true},
		{name: "malformed_base64", input: "!!!!", encoding: "base64", limit: 10, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			structure := &imap.BodyStructure{MIMEType: "text", MIMESubType: tc.subtype, Encoding: tc.encoding, Params: map[string]string{"charset": tc.charset}}
			text, decoded, truncated, complete, err := DecodeTextPart(strings.NewReader(tc.input), structure, tc.limit)
			if tc.wantError {
				if err == nil || text != "" {
					t.Fatalf("invalid body accepted: %q, %v", text, err)
				}
				return
			}
			if err != nil || text != tc.want || decoded != tc.decoded || truncated != tc.truncated || complete != tc.complete {
				t.Fatalf("text=%q decoded=%d truncated=%t complete=%t error=%v", text, decoded, truncated, complete, err)
			}
			if !utf8.ValidString(text) || len(text) > tc.limit {
				t.Fatal("output violated UTF-8 byte budget")
			}
		})
	}
	if _, _, _, _, err := DecodeTextPart(strings.NewReader("hello"), nil, 10); err == nil {
		t.Fatal("missing metadata accepted")
	}
	if _, _, _, _, err := DecodeTextPart(failedBodyReader{io.ErrUnexpectedEOF}, &imap.BodyStructure{}, 10); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("reader error identity lost: %v", err)
	}
}

func TestHTMLTextExcludesActiveContentAndUnsafeLinks(t *testing.T) {
	for _, tag := range []string{"head", "title", "script", "style", "noscript", "template", "svg", "iframe", "object", "form"} {
		t.Run(tag, func(t *testing.T) {
			content := "discard-marker"
			if tag == "head" {
				content = "<title>discard-marker</title>"
			}
			text, err := HTMLToText("<" + tag + ">" + content + "</" + tag + "><p>visible</p>")
			if err != nil || strings.Contains(text, "discard-marker") || !strings.Contains(text, "visible") {
				t.Fatalf("active content projection = %q, %v", text, err)
			}
		})
	}
	for _, href := range []string{"https://example.test", "http://example.test", "mailto:help@example.test", "javascript:alert(1)", "data:text/plain,hidden", "file:///secret"} {
		t.Run(href, func(t *testing.T) {
			text, err := HTMLToText(`<a href="` + href + `">visible</a>`)
			allowed := strings.HasPrefix(href, "https:") || strings.HasPrefix(href, "http:") || strings.HasPrefix(href, "mailto:")
			if err != nil || !strings.Contains(text, "visible") || strings.Contains(text, href) != allowed {
				t.Fatalf("link projection = %q, %v", text, err)
			}
		})
	}
}

func TestBodyWhitespaceAndAttachmentMetadataStayBounded(t *testing.T) {
	if got := NormalizeBodyWhitespace("\n\n first  \r\n\n\nsecond\t\n\n"); got != "first\n\nsecond" {
		t.Fatalf("whitespace = %q", got)
	}
	if got := BoundedText("a\x00b\rc\nd\te\x7f", 20); got != "ab c d e" {
		t.Fatalf("controls = %q", got)
	}
	if got := BoundedText("\u20ac\u20ac", 4); got != "\u20ac" || !utf8.ValidString(got) {
		t.Fatalf("bounded Unicode = %q", got)
	}
	if PartID(nil) != "1" || PartID([]int{1, 2, 3}) != "1.2.3" {
		t.Fatal("part path identity changed")
	}
	for _, value := range []string{"../report.txt", `folder\report.txt`} {
		if got := safeFilename(value); got != "report.txt" {
			t.Fatalf("filename = %q", got)
		}
	}
	if safeFilename(".") != "" || safeFilename("/") != "" {
		t.Fatal("directory-only filename accepted")
	}
}
