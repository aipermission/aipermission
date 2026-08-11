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
