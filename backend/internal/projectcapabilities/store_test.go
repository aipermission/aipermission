package projectcapabilities

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestReplaceAndEffectiveCapability(t *testing.T) {
	db := openCapabilityTestDB(t)
	ctx := context.Background()
	tokenID, projectID := seedCapabilityFixture(t, db)
	store := NewStore(db)

	items, err := store.Replace(ctx, tokenID, []SetInput{{
		ProjectID:     projectID,
		Name:          VaultMetadataRead,
		ExecutionRule: RuleAlwaysRun,
	}})
	if err != nil {
		t.Fatalf("replace capabilities: %v", err)
	}
	if len(items) != 1 || items[0].Name != VaultMetadataRead || !items[0].ProjectEnabled {
		t.Fatalf("unexpected capabilities: %#v", items)
	}
	effective, err := store.Effective(ctx, tokenID, projectID, VaultMetadataRead, time.Now())
	if err != nil || effective.ExecutionRule != RuleAlwaysRun {
		t.Fatalf("effective capability: %#v %v", effective, err)
	}
}

func TestCapabilityRulesAreDefinitionSpecific(t *testing.T) {
	db := openCapabilityTestDB(t)
	tokenID, projectID := seedCapabilityFixture(t, db)
	store := NewStore(db)

	for _, input := range []SetInput{
		{ProjectID: projectID, Name: VaultMetadataRead, ExecutionRule: RuleApprovalRequired},
		{ProjectID: projectID, Name: VaultMetadataRead, ExecutionRule: "blocked"},
		{ProjectID: projectID, Name: VaultItemGenerate, ExecutionRule: "blocked"},
		{ProjectID: projectID, Name: VaultSessionApply, ExecutionRule: "blocked"},
	} {
		if _, err := store.Replace(context.Background(), tokenID, []SetInput{input}); err == nil {
			t.Fatalf("expected invalid rule to fail: %#v", input)
		}
	}

	for _, input := range []SetInput{
		{ProjectID: projectID, Name: VaultItemGenerate, ExecutionRule: RuleAlwaysRun},
		{ProjectID: projectID, Name: VaultSessionApply, ExecutionRule: RuleAlwaysRun},
	} {
		if _, err := store.Replace(context.Background(), tokenID, []SetInput{input}); err != nil {
			t.Fatalf("expected always rule to succeed: %#v: %v", input, err)
		}
	}
}

func TestDisabledProjectMakesCapabilityIneffective(t *testing.T) {
	db := openCapabilityTestDB(t)
	ctx := context.Background()
	tokenID, projectID := seedCapabilityFixture(t, db)
	store := NewStore(db)
	if _, err := store.Replace(ctx, tokenID, []SetInput{{
		ProjectID: projectID, Name: VaultMetadataRead, ExecutionRule: RuleAlwaysRun,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE token_project_scopes SET enabled = 0 WHERE token_id = ? AND project_id = ?`, tokenID, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Effective(ctx, tokenID, projectID, VaultMetadataRead, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected disabled project to hide capability, got %v", err)
	}
}

func TestReplaceRejectsExpiredAndDuplicateCapabilities(t *testing.T) {
	db := openCapabilityTestDB(t)
	tokenID, projectID := seedCapabilityFixture(t, db)
	store := NewStore(db)
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if _, err := store.Replace(context.Background(), tokenID, []SetInput{{
		ProjectID: projectID, Name: VaultMetadataRead, ExecutionRule: RuleAlwaysRun, ExpiresAt: past,
	}}); err == nil {
		t.Fatal("expected expired capability to fail")
	}
	input := SetInput{ProjectID: projectID, Name: VaultMetadataRead, ExecutionRule: RuleAlwaysRun}
	if _, err := store.Replace(context.Background(), tokenID, []SetInput{input, input}); err == nil {
		t.Fatal("expected duplicate capability to fail")
	}
}

func TestCapabilityRevisionDoesNotResetAfterRemoval(t *testing.T) {
	db := openCapabilityTestDB(t)
	ctx := context.Background()
	tokenID, projectID := seedCapabilityFixture(t, db)
	store := NewStore(db)
	input := SetInput{ProjectID: projectID, Name: VaultMetadataRead, ExecutionRule: RuleAlwaysRun}
	first, err := store.Replace(ctx, tokenID, []SetInput{input})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Replace(ctx, tokenID, nil); err != nil {
		t.Fatal(err)
	}
	second, err := store.Replace(ctx, tokenID, []SetInput{input})
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Revision <= first[0].Revision {
		t.Fatalf("revision reset after removal: first=%d second=%d", first[0].Revision, second[0].Revision)
	}
}

func TestReplaceDoesNotBumpRevisionForIdenticalCapabilities(t *testing.T) {
	db := openCapabilityTestDB(t)
	ctx := context.Background()
	tokenID, projectID := seedCapabilityFixture(t, db)
	store := NewStore(db)
	input := SetInput{ProjectID: projectID, Name: VaultSessionApply, ExecutionRule: RuleAlwaysRun}
	first, changed, err := store.ReplaceWithChange(ctx, tokenID, []SetInput{input})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("initial capability replace was reported unchanged")
	}
	second, changed, err := store.ReplaceWithChange(ctx, tokenID, []SetInput{input})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("identical capability replace was reported changed")
	}
	if len(first) != 1 || len(second) != 1 || second[0].Revision != first[0].Revision {
		t.Fatalf("no-op replace changed revision: first=%#v second=%#v", first, second)
	}
}

func seedCapabilityFixture(t *testing.T, db *sql.DB) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	token, err := tokens.NewStore(db).Create(ctx, tokens.CreateRequest{Name: "capability-test"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	project, err := projects.NewStore(db).Create(ctx, "My Project")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return token.ID, project.ID
}

func openCapabilityTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "capabilities.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
