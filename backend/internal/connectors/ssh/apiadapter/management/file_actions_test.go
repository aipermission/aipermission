package management

import (
	"fmt"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

func TestBrowseRemoteFilesOutputEnforcesActionRowLimit(t *testing.T) {
	entries := make([]connectors.RemoteFileEntry, sshconnector.MaxBrowseRemoteRows+2)
	for index := range entries {
		entries[index] = connectors.RemoteFileEntry{Name: fmt.Sprintf("entry-%03d", index)}
	}

	output := browseRemoteFilesOutput(17, "/tmp", entries)
	bounded, ok := output["entries"].([]connectors.RemoteFileEntry)
	if !ok {
		t.Fatalf("entries type = %T", output["entries"])
	}
	if len(bounded) != sshconnector.MaxBrowseRemoteRows {
		t.Fatalf("entries = %d, want %d", len(bounded), sshconnector.MaxBrowseRemoteRows)
	}
	if output["total_entries"] != sshconnector.MaxBrowseRemoteRows+2 || output["max_rows"] != sshconnector.MaxBrowseRemoteRows {
		t.Fatalf("unexpected row metadata: %#v", output)
	}
	if output["truncated"] != true || output["has_more"] != true {
		t.Fatalf("missing truncation metadata: %#v", output)
	}
}

func TestBrowseRemoteFilesOutputReportsCompleteResults(t *testing.T) {
	entries := []connectors.RemoteFileEntry{{Name: "one"}}
	output := browseRemoteFilesOutput(17, "/tmp", entries)
	if output["truncated"] != false || output["has_more"] != false || output["total_entries"] != 1 {
		t.Fatalf("unexpected complete-result metadata: %#v", output)
	}
}
