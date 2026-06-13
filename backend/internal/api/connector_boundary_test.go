package api

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenericConnectorHandlersDoNotBranchOnSSH(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	for _, file := range []string{
		"connector_target_handlers.go",
		"target_handlers.go",
		"../history/store.go",
		"../console/console_session_manager.go",
		"../filetransfer/store.go",
	} {
		sourcePath := filepath.Join(filepath.Dir(filename), file)
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		source := string(content)
		for _, disallowed := range []string{
			"connectors/ssh",
			"sshconnector",
			"connector_kind = 'ssh'",
			"connector_kind='ssh'",
			"ConnectorKind ==",
			"ConnectorKind !=",
		} {
			if strings.Contains(source, disallowed) {
				t.Fatalf("%s must use connector adapters, found %q", file, disallowed)
			}
		}
	}
}
