package identity

import (
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestInitializeBuildsRecordBoundWorkspaceIdentity(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "identity.db"), "IdentityPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	state, err := Initialize(t.Context(), database, "IdentityPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if state.GatewaySecret == "" || state.WorkspaceUUID == "" || state.UIRetryIdentity == "" || state.RuntimeInstanceID == "" || len(state.ActionIdentityKey) == 0 || state.Vault == nil {
		t.Fatal("workspace identity is incomplete")
	}
}
