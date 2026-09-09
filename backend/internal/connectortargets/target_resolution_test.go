package connectortargets

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projects"
)

func TestResolveTargetProfileViewsPreservesCompletePublicIdentity(t *testing.T) {
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "target-resolution.db"), "TargetResolutionPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	project, err := projects.NewStore(database).Create(ctx, "Resolution project")
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	target, err := store.CreateTarget(ctx, CreateTargetInput{
		ProjectID: project.ID, ConnectorKind: "test", Name: "Resolution target",
		Config: map[string]any{"endpoint": "local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(ctx, CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "operator", Label: "main",
		Public: map[string]any{"username": "operator"},
	})
	if err != nil {
		t.Fatal(err)
	}

	targetView, profileView, err := store.ResolveTargetProfileViews(ctx, target.ID, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if targetView.ID != target.ID || targetView.ProjectID != project.ID || targetView.Ref != connectors.FormatTargetRef("test", target.ID, profile.ID) ||
		targetView.ConnectorKind != "test" || targetView.Name != target.Name || targetView.UpdatedAt != target.UpdatedAt ||
		targetView.Config["endpoint"] != "local" {
		t.Fatalf("target view = %#v", targetView)
	}
	if profileView.ID != profile.ID || profileView.TargetID != target.ID || profileView.ConnectorKind != "test" ||
		profileView.Kind != "operator" || profileView.Label != "main" || profileView.Public["username"] != "operator" {
		t.Fatalf("profile view = %#v", profileView)
	}
}
