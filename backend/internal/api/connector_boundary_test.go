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

func TestSSHSpecificAPIReferencesStayInsideConnectorAdapters(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	apiDir := filepath.Dir(filename)
	disallowed := []string{
		"connectors/ssh",
		"sshconnector",
		"SSH connector",
		"SSH exec",
		"unsupported SSH",
		"connector_kind = 'ssh'",
		"connector_kind='ssh'",
	}
	err := filepath.WalkDir(apiDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		name := filepath.Base(path)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(content)
		for _, pattern := range disallowed {
			if strings.Contains(source, pattern) {
				t.Fatalf("%s must keep SSH behavior behind connector adapters, found %q", name, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api dir: %v", err)
	}
}
