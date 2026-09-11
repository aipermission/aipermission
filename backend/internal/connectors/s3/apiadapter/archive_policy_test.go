package apiadapter

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestS3AcceptedArchiveComponentsSurviveActualWriter(t *testing.T) {
	policy := New().(connectorapi.FileTransferPathPolicy)
	for _, value := range []string{"/daily/report.txt", "/nested/caf\u00e9", "/nested/cafe\u0301", "/a b/file.txt", "/" + strings.Repeat("x", 160)} {
		if err := policy.ValidateDownloadPaths([]string{value}); err != nil {
			t.Fatalf("ordinary mapping rejected: %v", err)
		}
		if mapped := filetransfer.RelativeArchiveEntryPath(value, ""); mapped != strings.TrimPrefix(value, "/") {
			t.Fatalf("accepted %q maps to %q", value, mapped)
		}
	}
}
