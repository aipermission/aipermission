package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
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

func TestReplaceTokenScopesUsesMonotonicRevisions(t *testing.T) {
	database := openProjectTestDB(t)
	ctx := t.Context()
	store := NewStore(database)
	project, err := store.Create(ctx, "Revision Project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "revision-token"})
	if err != nil {
		t.Fatal(err)
	}

	initial := scopeForProject(t, store, token.ID, project.ID)
	if !initial.Enabled || initial.Revision != 1 {
		t.Fatalf("initial scope = %#v", initial)
	}
	if _, err := store.ReplaceTokenScopes(ctx, token.ID, nil); err != nil {
		t.Fatal(err)
	}
	disabled := scopeForProject(t, store, token.ID, project.ID)
	if disabled.Enabled || disabled.Revision != initial.Revision+1 {
		t.Fatalf("disabled scope = %#v, initial = %#v", disabled, initial)
	}
	if _, err := store.ReplaceTokenScopes(ctx, token.ID, []int64{project.ID}); err != nil {
		t.Fatal(err)
	}
	reenabled := scopeForProject(t, store, token.ID, project.ID)
	if !reenabled.Enabled || reenabled.Revision != disabled.Revision+1 {
		t.Fatalf("re-enabled scope = %#v, disabled = %#v", reenabled, disabled)
	}
	if _, err := store.ReplaceTokenScopes(ctx, token.ID, []int64{project.ID}); err != nil {
		t.Fatal(err)
	}
	unchanged := scopeForProject(t, store, token.ID, project.ID)
	if unchanged.Revision != reenabled.Revision {
		t.Fatalf("no-op revision = %d, want %d", unchanged.Revision, reenabled.Revision)
	}
}

func TestResolveRefAcceptsActiveIDOrSlug(t *testing.T) {
	database := openProjectTestDB(t)
	store := NewStore(database)
	project, err := store.Create(t.Context(), "Resolve Me")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		fmt.Sprintf(" %d ", project.ID),
		fmt.Sprintf(" id:%d ", project.ID),
		" resolve-me ",
		" slug:resolve-me ",
	} {
		resolved, err := store.ResolveRef(t.Context(), ref)
		if err != nil {
			t.Fatalf("ResolveRef(%q): %v", ref, err)
		}
		if resolved.ID != project.ID {
			t.Fatalf("ResolveRef(%q) ID = %d, want %d", ref, resolved.ID, project.ID)
		}
	}
	if _, err := store.ResolveRef(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ResolveRef() error = %v", err)
	}
	for _, ref := range []string{"id:", "id:0", "id:not-a-number", "slug:"} {
		if _, err := store.ResolveRef(t.Context(), ref); err == nil {
			t.Fatalf("ResolveRef(%q) unexpectedly succeeded", ref)
		}
	}
}

func TestNumericProjectNamesUseUnambiguousSlugs(t *testing.T) {
	store := NewStore(openProjectTestDB(t))
	for name, want := range map[string]string{
		"2026":   "project-2026",
		" 0007 ": "project-0007",
		"2026!":  "project-2026",
	} {
		project, err := store.Create(t.Context(), name)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		if project.Slug != want && !strings.HasPrefix(project.Slug, want+"-") {
			t.Fatalf("project %q slug = %q, want base %q", name, project.Slug, want)
		}
	}
}

func TestResolveRefRejectsLegacyNumericSlugCollision(t *testing.T) {
	database := openProjectTestDB(t)
	store := NewStore(database)
	byID, err := store.Create(t.Context(), "ID Project")
	if err != nil {
		t.Fatal(err)
	}
	bySlug, err := store.Create(t.Context(), "Slug Project")
	if err != nil {
		t.Fatal(err)
	}
	numeric := strconv.FormatInt(byID.ID, 10)
	if _, err := database.Exec(`UPDATE projects SET slug = ? WHERE id = ?`, numeric, bySlug.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveRef(t.Context(), numeric); !errors.Is(err, ErrAmbiguousRef) {
		t.Fatalf("ambiguous numeric ref error = %v", err)
	}
	resolvedID, err := store.ResolveRef(t.Context(), "id:"+numeric)
	if err != nil || resolvedID.ID != byID.ID {
		t.Fatalf("explicit id ref = %#v, err=%v", resolvedID, err)
	}
	resolvedSlug, err := store.ResolveRef(t.Context(), "slug:"+numeric)
	if err != nil || resolvedSlug.ID != bySlug.ID {
		t.Fatalf("explicit slug ref = %#v, err=%v", resolvedSlug, err)
	}
}

func TestResolveRefSupportsLegacyNumericSlugsOutsideInt64(t *testing.T) {
	database := openProjectTestDB(t)
	store := NewStore(database)
	project, err := store.Create(t.Context(), "Legacy Numeric")
	if err != nil {
		t.Fatal(err)
	}
	const numericSlug = "9223372036854775808"
	if _, err := database.Exec(`UPDATE projects SET slug = ? WHERE id = ?`, numericSlug, project.ID); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{numericSlug, "slug:" + numericSlug} {
		resolved, err := store.ResolveRef(t.Context(), ref)
		if err != nil || resolved.ID != project.ID {
			t.Fatalf("ResolveRef(%q) = %#v, err=%v", ref, resolved, err)
		}
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

func scopeForProject(t *testing.T, store *Store, tokenID, projectID int64) TokenScope {
	t.Helper()
	scopes, err := store.ListTokenScopes(t.Context(), tokenID)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range scopes {
		if scope.ProjectID == projectID {
			return scope
		}
	}
	t.Fatalf("scope for project %d not found", projectID)
	return TokenScope{}
}
