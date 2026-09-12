package mailconnector

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	mailcontent "github.com/aipermission/aipermission/backend/internal/connectors/mail/content"
	"github.com/emersion/go-imap"
)

func TestPreferredTextPartChoosesPlainBeforeHTML(t *testing.T) {
	structure := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "alternative", Parts: []*imap.BodyStructure{
		{MIMEType: "text", MIMESubType: "html", Size: 20},
		{MIMEType: "text", MIMESubType: "plain", Size: 10},
	}}
	parts := mailcontent.PreferredTextParts(structure)
	if len(parts) == 0 || parts[0].Structure.MIMESubType != "plain" || mailcontent.PartID(parts[0].Path) != "2" {
		t.Fatalf("parts = %#v", parts)
	}
}

func TestPreferredTextPartSkipsAttachmentPartsAndTheirChildren(t *testing.T) {
	structure := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Parts: []*imap.BodyStructure{
		{MIMEType: "multipart", MIMESubType: "mixed", Disposition: "attachment", DispositionParams: map[string]string{"filename": "forwarded.eml"}, Parts: []*imap.BodyStructure{{MIMEType: "text", MIMESubType: "plain", Size: 99}}},
		{MIMEType: "text", MIMESubType: "plain", Size: 10},
	}}
	parts := mailcontent.PreferredTextParts(structure)
	if len(parts) == 0 || mailcontent.PartID(parts[0].Path) != "2" {
		t.Fatalf("parts = %#v", parts)
	}
}

func TestDecodeTextPartBoundsBase64AndNormalizesHTML(t *testing.T) {
	source := `<p>Hello <strong>team</strong>.</p><script>steal()</script><p><a href="https://example.com/run">Runbook</a></p><img src="https://tracker.invalid/pixel">`
	encoded := base64.StdEncoding.EncodeToString([]byte(source))
	text, decoded, truncated, complete, err := mailcontent.DecodeTextPart(strings.NewReader(encoded), &imap.BodyStructure{
		MIMEType: "text", MIMESubType: "html", Encoding: "base64", Params: map[string]string{"charset": "utf-8"},
	}, maxBodyBytes)
	if err != nil {
		t.Fatalf("decode HTML: %v", err)
	}
	if decoded != len(source) || truncated || !complete {
		t.Fatalf("decoded=%d truncated=%v complete=%v", decoded, truncated, complete)
	}
	if strings.Contains(text, "steal") || strings.Contains(text, "tracker") || !strings.Contains(text, "Runbook (https://example.com/run)") {
		t.Fatalf("text = %q", text)
	}

	plain, _, truncated, complete, err := mailcontent.DecodeTextPart(strings.NewReader(strings.Repeat("x", 64)), &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain", Encoding: "8bit"}, 16)
	if err != nil || len(plain) != 16 || !truncated || complete {
		t.Fatalf("plain length=%d truncated=%v complete=%v err=%v", len(plain), truncated, complete, err)
	}
}

func TestDecodeHTMLReboundsExpandedTextProjection(t *testing.T) {
	source := strings.Repeat(`<a href="https://example.com/very/long/link">x</a>`, 100)
	text, _, truncated, complete, err := mailcontent.DecodeTextPart(strings.NewReader(source), &imap.BodyStructure{MIMEType: "text", MIMESubType: "html", Encoding: "8bit"}, 128)
	if err != nil || len(text) > 128 || !truncated || complete {
		t.Fatalf("text bytes=%d truncated=%v complete=%v err=%v", len(text), truncated, complete, err)
	}
}

func TestDecodeTextPartRejectsMalformedTransferEncodingAndConvertsKnownCharset(t *testing.T) {
	if _, _, _, _, err := mailcontent.DecodeTextPart(strings.NewReader("%%%"), &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain", Encoding: "base64"}, maxBodyBytes); err == nil {
		t.Fatal("expected malformed base64 rejection")
	}
	text, _, _, _, err := mailcontent.DecodeTextPart(strings.NewReader("caf\xe9"), &imap.BodyStructure{
		MIMEType: "text", MIMESubType: "plain", Encoding: "8bit", Params: map[string]string{"charset": "iso-8859-1"},
	}, maxBodyBytes)
	if err != nil || text != "caf\u00e9" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestDecodeTextPartBoundsUnsupportedTransferEncodingErrors(t *testing.T) {
	encoding := strings.Repeat("server-controlled-encoding", 10000)
	_, _, _, _, err := mailcontent.DecodeTextPart(strings.NewReader("body"), &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain", Encoding: encoding}, maxBodyBytes)
	if connectors.ErrorCode(err) != "unsupported_transfer_encoding" || len(err.Error()) > 128 || strings.Contains(err.Error(), encoding[:128]) {
		t.Fatalf("unsupported encoding error was not bounded: code=%q len=%d err=%v", connectors.ErrorCode(err), len(err.Error()), err)
	}
}

func TestAttachmentRowsAreBoundedAndFilenamesAreDisplayOnly(t *testing.T) {
	parts := make([]*imap.BodyStructure, 0, mailcontent.MaxAttachmentRows+10)
	for index := 0; index < mailcontent.MaxAttachmentRows+10; index++ {
		parts = append(parts, &imap.BodyStructure{
			MIMEType: "application", MIMESubType: "octet-stream", Size: 42,
			Disposition: "attachment", DispositionParams: map[string]string{"filename": "../../secret.txt"},
		})
	}
	rows, _, _, truncated := mailcontent.AttachmentRows(&imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Parts: parts})
	if len(rows) != mailcontent.MaxAttachmentRows || !truncated {
		t.Fatalf("rows = %d truncated=%v", len(rows), truncated)
	}
	if rows[0]["filename"] != "secret.txt" {
		t.Fatalf("filename = %#v", rows[0]["filename"])
	}
}

func TestPreferredTextPartsPreserveHTMLFallbackAfterPlainText(t *testing.T) {
	parts := mailcontent.PreferredTextParts(&imap.BodyStructure{MIMEType: "multipart", MIMESubType: "alternative", Parts: []*imap.BodyStructure{
		{MIMEType: "text", MIMESubType: "plain", Size: 0},
		{MIMEType: "text", MIMESubType: "html", Size: 12},
	}})
	if len(parts) != 2 || parts[0].Structure.MIMESubType != "plain" || parts[1].Structure.MIMESubType != "html" {
		t.Fatalf("parts = %#v", parts)
	}
}

func TestAttachmentRowsDistinguishSignedFromEncryptedContent(t *testing.T) {
	root := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "signed", Parts: []*imap.BodyStructure{
		{MIMEType: "text", MIMESubType: "plain"},
		{MIMEType: "application", MIMESubType: "pkcs7-signature", Disposition: "attachment"},
	}}
	_, encrypted, signed, _ := mailcontent.AttachmentRows(root)
	if encrypted || !signed {
		t.Fatalf("encrypted=%v signed=%v", encrypted, signed)
	}
}

type emptyUIDSearchClient struct{}

func (emptyUIDSearchClient) UidSearch(*imap.SearchCriteria) ([]uint32, error) { return nil, nil }

type denseUIDSearchClient struct{}

func (denseUIDSearchClient) UidSearch(*imap.SearchCriteria) ([]uint32, error) {
	return []uint32{2000, 1999, 1998}, nil
}

func TestBoundedUIDSearchPreservesFinalWindowMatches(t *testing.T) {
	uids, scanned, nextBeforeUID, exhausted, err := boundedUIDSearch(t.Context(), denseUIDSearchClient{}, normalizedSearch{Folder: "INBOX"}, 2000, 2, maxUIDsScanned)
	if err != nil || len(uids) != 3 || scanned != uidSearchWindow || nextBeforeUID != 1001 || exhausted {
		t.Fatalf("uids=%v scanned=%d next=%d exhausted=%v err=%v", uids, scanned, nextBeforeUID, exhausted, err)
	}
}

func TestBoundedUIDSearchReturnsCursorAfterSparseScanLimit(t *testing.T) {
	uids, scanned, nextBeforeUID, exhausted, err := boundedUIDSearch(t.Context(), emptyUIDSearchClient{}, normalizedSearch{Folder: "INBOX"}, 20000, 51, maxUIDsScanned)
	if err != nil || len(uids) != 0 || scanned != maxUIDsScanned || nextBeforeUID != 10001 || exhausted {
		t.Fatalf("uids=%v scanned=%d next=%d exhausted=%v err=%v", uids, scanned, nextBeforeUID, exhausted, err)
	}
}

func TestSearchCursorRejectsTamperingAndContextDrift(t *testing.T) {
	search := normalizedSearch{Folder: "INBOX", UnreadOnly: true, Subject: "incident"}
	hash, err := searchFingerprint(search)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	encoded, err := encodeSearchCursor(searchCursor{Version: 1, Order: "uid_desc", Folder: "INBOX", UIDValidity: 7, QueryHash: hash, NextBeforeUID: 40})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := decodeSearchCursor(encoded)
	if err != nil || decoded.NextBeforeUID != 40 {
		t.Fatalf("decoded=%#v err=%v", decoded, err)
	}
	if _, err := decodeSearchCursor(encoded + "!"); err == nil {
		t.Fatal("expected malformed cursor rejection")
	}
	if _, err := decodeSearchCursor(strings.Repeat("x", maxCursorBytes+1)); err == nil {
		t.Fatal("expected oversized cursor rejection before decode")
	}
}

func FuzzHTMLToTextNeverReturnsActiveMarkup(f *testing.F) {
	f.Add(`<p>Hello</p><script>alert(1)</script>`)
	f.Add(`<a href="javascript:alert(1)">bad</a>`)
	f.Add(`jAvAsCript:0`)
	f.Fuzz(func(t *testing.T, source string) {
		const marker = "__aipermission_active_content_marker__"
		if len(source) > mailcontent.MaxHTMLBodyBytes-256 || strings.Contains(source, marker) {
			t.Skip()
		}
		wrapped := `<script>` + marker + `</script><a href="jAvAsCrIpT:` + marker + `">safe</a>` + source
		text, err := mailcontent.HTMLToText(wrapped)
		if err != nil {
			return
		}
		if strings.Contains(text, marker) {
			t.Fatalf("active content survived: %q", text)
		}
	})
}
