package api

import (
	"mime/multipart"
	"net/textproto"
	"testing"
)

func TestTransferPathPolicyFallbackRetainsFilesystemRules(t *testing.T) {
	adapter := struct{}{}
	for _, test := range []struct {
		name string
		want string
	}{
		{"folder//report.txt", "/base/folder/report.txt"},
		{"folder/../report.txt", "/base/report.txt"},
		{" folder\\report.txt ", "/base/folder/report.txt"},
	} {
		got, err := transferUploadPath(adapter, "/base", test.name)
		if err != nil || got != test.want {
			t.Fatalf("join %q = %q, %v; want %q", test.name, got, err, test.want)
		}
	}
	for _, name := range []string{"../escape", "/absolute", "folder/../../escape", "a\ninvalid"} {
		if _, err := transferUploadPath(adapter, "/base", name); err == nil {
			t.Fatalf("unsafe relative path %q accepted", name)
		}
	}
	if got := transferParent(adapter, "/base/folder"); got != "/base" {
		t.Fatalf("parent = %q", got)
	}
	header := &multipart.FileHeader{Filename: " .report. ", Header: textproto.MIMEHeader{}}
	header.Header.Set("Content-Disposition", `form-data; name="files"; filename="a/../different"`)
	got, err := transferUploadFilename(adapter, header)
	if err != nil || got != "report" {
		t.Fatalf("non-opaque filename = %q, %v", got, err)
	}
}
