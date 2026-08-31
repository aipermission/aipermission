package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectorSecretAccessorIdentifiesMissingOptionalSecrets(t *testing.T) {
	_, err := (connectorSecretAccessor{values: map[string]any{}}).GetSecret(context.Background(), "password")
	if !errors.Is(err, connectors.ErrSecretNotFound) {
		t.Fatalf("missing secret error = %v, want ErrSecretNotFound", err)
	}
}

func TestGenericConnectorHandlersDoNotBranchOnSSH(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	apiDir := filepath.Dir(filename)
	targetFiles, err := filepath.Glob(filepath.Join(apiDir, "connector_target_*.go"))
	if err != nil {
		t.Fatalf("find connector target handlers: %v", err)
	}
	sourcePaths := append(targetFiles,
		filepath.Join(apiDir, "target_handlers.go"),
		filepath.Join(apiDir, "../history/store.go"),
		filepath.Join(apiDir, "../console/console_session_manager.go"),
		filepath.Join(apiDir, "../filetransfer/store.go"),
	)
	for _, sourcePath := range sourcePaths {
		if strings.HasSuffix(sourcePath, "_test.go") {
			continue
		}
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read %s: %v", sourcePath, err)
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
				t.Fatalf("%s must use connector adapters, found %q", sourcePath, disallowed)
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

func TestProvisionedCredentialMetadataStaysInsideConnectors(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	apiDir := filepath.Dir(filename)
	disallowed := []string{
		"managed_by_aipermission",
		"managed_role_name",
		"managed_admin_profile_id",
		"managed_admin_profile_ref",
		"managed_preset",
		"managed_scope",
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
				t.Fatalf("%s must keep provisioned credential metadata behind connector contracts, found %q", name, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api dir: %v", err)
	}
}
