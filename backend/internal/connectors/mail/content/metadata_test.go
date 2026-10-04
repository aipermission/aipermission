package mailcontent

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap"
)

func TestPreferredTextSkipsAttachmentsAndRetainsCanonicalPaths(t *testing.T) {
	plain := &imap.BodyStructure{MIMEType: "TEXT", MIMESubType: "PLAIN"}
	html := &imap.BodyStructure{MIMEType: "text", MIMESubType: "html"}
	attachment := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Disposition: "attachment", Parts: []*imap.BodyStructure{
		{MIMEType: "text", MIMESubType: "plain"},
	}}
	root := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Parts: []*imap.BodyStructure{attachment, html, plain}}
	parts := PreferredTextParts(root)
	if len(parts) != 2 || parts[0].Structure != plain || parts[1].Structure != html || PartID(parts[0].Path) != "3" || PartID(parts[1].Path) != "2" {
		t.Fatalf("preferred text paths = %+v", parts)
	}
	if got := PreferredTextParts(nil); len(got) != 0 {
		t.Fatal("missing MIME tree produced text parts")
	}
}

func TestAttachmentRowsBoundMetadataWithoutDroppingCryptoFlags(t *testing.T) {
	attachment := &imap.BodyStructure{
		MIMEType: "application", MIMESubType: "octet-stream", Disposition: "ATTACHMENT",
		DispositionParams: map[string]string{"filename": `folder\report.txt`}, Size: 42,
		Id: "line1\nline2",
	}
	root := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Parts: []*imap.BodyStructure{
		{MIMEType: "text", MIMESubType: "plain"}, attachment,
		{MIMEType: "application", MIMESubType: "pgp-encrypted"},
		{MIMEType: "application", MIMESubType: "pgp-signature"},
	}}
	rows, encrypted, signed, limited := AttachmentRows(root)
	if len(rows) != 3 || !encrypted || !signed || limited {
		t.Fatalf("rows=%d encrypted=%t signed=%t limited=%t", len(rows), encrypted, signed, limited)
	}
	row := rows[0]
	if row["filename"] != "report.txt" || row["part_id"] != "2" || row["content_type"] != "application/octet-stream" || row["declared_size_bytes"] != attachment.Size || row["decoded_size_bytes"] != nil || row["content_id"] != "line1 line2" {
		t.Fatalf("attachment metadata = %+v", row)
	}
	long := safeFilename(strings.Repeat("x", maxFilenameBytes+1))
	if len(long) != maxFilenameBytes {
		t.Fatalf("filename bytes = %d", len(long))
	}
	root.Parts = make([]*imap.BodyStructure, MaxAttachmentRows+1)
	for index := range root.Parts {
		root.Parts[index] = attachment
	}
	root.Parts = append(root.Parts,
		&imap.BodyStructure{MIMEType: "application", MIMESubType: "pgp-encrypted"},
		&imap.BodyStructure{MIMEType: "application", MIMESubType: "pgp-signature"},
	)
	rows, encrypted, signed, limited = AttachmentRows(root)
	if len(rows) != MaxAttachmentRows || !encrypted || !signed || !limited {
		t.Fatalf("attachment cap = %d encrypted=%t signed=%t limited=%t", len(rows), encrypted, signed, limited)
	}
}
