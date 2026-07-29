package projects

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestProjectLifecycleAndTokenScopeDefaults(t *testing.T) {
	database := openProjectTestDB(t)
	store := NewStore(database)
	ctx := context.Background()

	ungrouped, err := store.Ungrouped(ctx)
	if err != nil {
		t.Fatalf("get ungrouped project: %v", err)
	}
	if ungrouped.Slug != UngroupedSlug {
		t.Fatalf("ungrouped slug = %q", ungrouped.Slug)
	}

	tokenResponse, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "project-test"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	project, err := store.Create(ctx, "Project Alpha")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if project.Slug != "project-alpha" {
		t.Fatalf("project slug = %q", project.Slug)
	}

	scopes, err := store.ListTokenScopes(ctx, tokenResponse.ID)
	if err != nil {
		t.Fatalf("list token project scopes: %v", err)
	}
	if len(scopes) != 2 || !scopeEnabled(scopes, ungrouped.ID) || !scopeEnabled(scopes, project.ID) {
		t.Fatalf("unexpected default project scopes: %#v", scopes)
	}

	updated, err := store.Update(ctx, project.ID, "Renamed Project")
	if err != nil {
		t.Fatalf("rename project: %v", err)
	}
	if updated.Slug != project.Slug || updated.Name != "Renamed Project" {
		t.Fatalf("rename changed stable identity: %#v", updated)
	}

	scopes, err = store.ReplaceTokenScopes(ctx, tokenResponse.ID, []int64{project.ID})
	if err != nil {
		t.Fatalf("replace token scopes: %v", err)
	}
	if scopeEnabled(scopes, ungrouped.ID) || !scopeEnabled(scopes, project.ID) {
		t.Fatalf("unexpected replaced project scopes: %#v", scopes)
	}

	if err := store.Archive(ctx, ungrouped.ID); !errors.Is(err, ErrProtected) {
		t.Fatalf("archive ungrouped error = %v", err)
	}
	if err := store.Archive(ctx, project.ID); err != nil {
		t.Fatalf("archive empty project: %v", err)
	}
	recreated, err := store.Create(ctx, "Project Alpha")
	if err != nil {
		t.Fatalf("recreate archived project name: %v", err)
	}
	if recreated.Slug != "project-alpha-2" {
		t.Fatalf("archived project slug was reused: %q", recreated.Slug)
	}
}

func TestReplaceTokenScopesReportsNoOp(t *testing.T) {
	db := openProjectTestDB(t)
	ctx := context.Background()
	store := NewStore(db)
	project, err := store.Create(ctx, "Scope Project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(db).Create(ctx, tokens.CreateRequest{Name: "scope-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, changed, err := store.ReplaceTokenScopesWithChange(ctx, token.ID, []int64{project.ID})
	if err != nil || !changed {
		t.Fatalf("initial scope replace: changed=%v err=%v", changed, err)
	}
	_, changed, err = store.ReplaceTokenScopesWithChange(ctx, token.ID, []int64{project.ID})
	if err != nil || changed {
		t.Fatalf("identical scope replace: changed=%v err=%v", changed, err)
	}
}

func TestProjectCannotBeArchivedWithActiveTargets(t *testing.T) {
	database := openProjectTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	project, err := store.Create(ctx, "Project Beta")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO connector_targets (project_id, connector_kind, name, config_json, status, created_at, updated_at)
		VALUES (?, 'postgres', 'main-db', '{}', 'active', datetime('now'), datetime('now'))`, project.ID); err != nil {
		t.Fatalf("insert project target: %v", err)
	}
	if err := store.Archive(ctx, project.ID); !errors.Is(err, ErrProjectNotEmpty) {
		t.Fatalf("archive populated project error = %v", err)
	}
}

func openProjectTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "projects.db"), "correct horse battery staple")
	if err != nil {
		t.Fatalf("open project test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func scopeEnabled(scopes []TokenScope, projectID int64) bool {
	for _, scope := range scopes {
		if scope.ProjectID == projectID {
			return scope.Enabled
		}
	}
	return false
}
