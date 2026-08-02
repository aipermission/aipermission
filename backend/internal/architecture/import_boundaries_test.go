package architecture

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/aipermission/aipermission/backend"

func TestConnectorGroundworkImportBoundaries(t *testing.T) {
	packages := []string{
		modulePath + "/internal/connectors",
		modulePath + "/internal/actions",
		modulePath + "/internal/connectortargets",
	}
	forbidden := []string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/db",
		modulePath + "/internal/execution",
		modulePath + "/internal/filetransfer",
		modulePath + "/internal/sshconfig",
		modulePath + "/internal/sshkeys",
		modulePath + "/internal/tokens",
		modulePath + "/internal/vault",
	}

	for _, pkg := range packages {
		imports := packageDependencies(t, pkg)
		for _, forbiddenImport := range forbidden {
			if imports[forbiddenImport] {
				t.Fatalf("%s must not import %s", pkg, forbiddenImport)
			}
		}
	}
}

func TestMailConnectorImportBoundary(t *testing.T) {
	pkg := modulePath + "/internal/connectors/mail"
	imports := packageDependencies(t, pkg)
	for _, forbiddenImport := range []string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/connectortargets",
		modulePath + "/internal/db",
		modulePath + "/internal/execution",
		modulePath + "/internal/history",
		modulePath + "/internal/tokens",
		modulePath + "/internal/vault",
	} {
		if imports[forbiddenImport] {
			t.Fatalf("%s must not import %s", pkg, forbiddenImport)
		}
	}
}

func TestOnlyBuiltinRegistryImportsMailConnector(t *testing.T) {
	mailPackage := modulePath + "/internal/connectors/mail"
	allowedImporter := modulePath + "/internal/connectors/builtin"
	cmd := exec.Command("go", "list", "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./...")
	cmd.Dir = "../.."
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list ./... failed: %v\n%s", err, string(exitErr.Stderr))
		}
		t.Fatalf("go list ./... failed: %v", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		importer, imports, ok := strings.Cut(line, "|")
		if !ok || importer == allowedImporter {
			continue
		}
		for _, imported := range strings.Fields(imports) {
			if imported == mailPackage {
				t.Fatalf("%s imports %s; only %s may register the Mail connector", importer, mailPackage, allowedImporter)
			}
		}
	}
}

func TestSharedRuntimeHasNoMailSpecificBranches(t *testing.T) {
	backendRoot := filepath.Clean(filepath.Join("..", ".."))
	allowedRoots := []string{
		filepath.Clean(filepath.Join(backendRoot, "internal", "connectors", "mail")) + string(filepath.Separator),
	}
	registryPath := filepath.Clean(filepath.Join(backendRoot, "internal", "connectors", "builtin", "registry.go"))
	registryTestPath := filepath.Clean(filepath.Join(backendRoot, "internal", "connectors", "builtin", "registry_test.go"))
	architectureRoot := filepath.Clean(filepath.Join(backendRoot, "internal", "architecture")) + string(filepath.Separator)
	mailIdentifiers := []string{
		`"mail"`, "mailconnector", "imap_", "smtp_",
		`"list_folders"`, `"check_mailbox"`, `"search_messages"`, `"get_message"`,
		`"list_attachments"`, `"mark_read"`, `"mark_unread"`, `"move_message"`,
		`"archive_message"`, `"send_message"`, `"reply_message"`, `"delete_message"`,
	}
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		cleanPath := filepath.Clean(path)
		if cleanPath == registryPath || cleanPath == registryTestPath || strings.HasPrefix(cleanPath+string(filepath.Separator), architectureRoot) {
			return nil
		}
		for _, root := range allowedRoots {
			if strings.HasPrefix(cleanPath+string(filepath.Separator), root) {
				return nil
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := strings.ToLower(string(content))
		for _, identifier := range mailIdentifiers {
			if strings.Contains(source, identifier) {
				t.Errorf("shared runtime contains Mail-specific identifier %q in %s", identifier, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan shared runtime: %v", err)
	}
}

func packageDependencies(t *testing.T, pkg string) map[string]bool {
	t.Helper()

	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg)
	cmd.Dir = "../.."
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list %s failed: %v\n%s", pkg, err, string(exitErr.Stderr))
		}
		t.Fatalf("go list %s failed: %v", pkg, err)
	}

	imports := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == pkg {
			continue
		}
		imports[line] = true
	}
	return imports
}
