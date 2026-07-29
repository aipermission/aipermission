package projectvault

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appdb "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestStoreCreateListRevealReplaceAndDelete(t *testing.T) {
	ctx := context.Background()
	database, store := openTestStore(t)
	projects := projectstore.NewStore(database)
	owner, err := projects.Create(ctx, "Core Platform")
	if err != nil {
		t.Fatalf("create owner project: %v", err)
	}
	shared, err := projects.Create(ctx, "Observability")
	if err != nil {
		t.Fatalf("create shared project: %v", err)
	}

	item, err := store.Create(ctx, CreateInput{
		Name:             "CORE_API_ADMIN_KEY",
		Value:            "value-that-is-long-enough",
		OwnerProjectID:   owner.ID,
		SharedProjectIDs: []int64{shared.ID},
		SecretType:       "api_key",
		Provider:         "Internal API",
		Environment:      "production",
		Description:      "Administrative API access",
		Source:           "imported",
		Tags:             []string{"api", "production"},
		UsageNotes:       []UsageNote{{Location: "core-1: /opt/core/.env", Notes: "Runtime environment"}},
	})
	if err != nil {
		t.Fatalf("create vault item: %v", err)
	}
	if item.ValueVersion != 1 || item.MetadataRevision != 1 || item.EncryptionVersion != 1 {
		t.Fatalf("unexpected revisions: %#v", item)
	}
	if len(item.ProjectIDs) != 1 || item.ProjectIDs[0] != shared.ID {
		t.Fatalf("unexpected shared projects: %#v", item.ProjectIDs)
	}
	if len(item.Tags) != 2 || len(item.UsageNotes) != 1 {
		t.Fatalf("relations missing: %#v", item)
	}

	var rawEncrypted string
	if err := database.QueryRowContext(ctx, `SELECT encrypted_value FROM vault_items WHERE id = ?`, item.ID).Scan(&rawEncrypted); err != nil {
		t.Fatalf("read encrypted value: %v", err)
	}
	if rawEncrypted == "" || strings.Contains(rawEncrypted, "value-that-is-long-enough") {
		t.Fatalf("vault value was not encrypted: %q", rawEncrypted)
	}

	revealed, err := store.Reveal(ctx, item.ID)
	if err != nil {
		t.Fatalf("reveal vault item: %v", err)
	}
	if revealed != "value-that-is-long-enough" {
		t.Fatalf("revealed value = %q", revealed)
	}

	sharedItems, total, err := store.List(ctx, ListFilter{ProjectID: shared.ID, Query: "CORE"})
	if err != nil {
		t.Fatalf("list shared vault items: %v", err)
	}
	if total != 1 || len(sharedItems) != 1 || sharedItems[0].ID != item.ID {
		t.Fatalf("unexpected shared list: total=%d items=%#v", total, sharedItems)
	}

	replaced, err := store.ReplaceValue(ctx, ReplaceValueInput{
		ID:                   item.ID,
		Value:                "replacement-value-long-enough",
		ExpectedValueVersion: item.ValueVersion,
	})
	if err != nil {
		t.Fatalf("replace vault value: %v", err)
	}
	if replaced.ValueVersion != 2 {
		t.Fatalf("value version = %d", replaced.ValueVersion)
	}
	if _, err := store.ReplaceValue(ctx, ReplaceValueInput{
		ID:                   item.ID,
		Value:                "stale-replacement-long-enough",
		ExpectedValueVersion: item.ValueVersion,
	}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale replace error = %v", err)
	}
	revealed, err = store.Reveal(ctx, item.ID)
	if err != nil || revealed != "replacement-value-long-enough" {
		t.Fatalf("replacement reveal = %q, %v", revealed, err)
	}

	updated, err := store.UpdateMetadata(ctx, UpdateMetadataInput{
		ID:                       item.ID,
		ExpectedMetadataRevision: item.MetadataRevision,
		Name:                     "CORE_API_ADMIN_TOKEN",
		OwnerProjectID:           owner.ID,
		SharedProjectIDs:         []int64{shared.ID},
		SecretType:               "access_token",
		Provider:                 "Internal API",
		Environment:              "production",
		Description:              "Rotated administrative access",
		ExpiryWarningDays:        30,
		Tags:                     []string{"rotated"},
		UsageNotes:               []UsageNote{{Location: "release workflow"}},
	})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.MetadataRevision != 2 || updated.ValueVersion != replaced.ValueVersion || updated.Name != "CORE_API_ADMIN_TOKEN" {
		t.Fatalf("unexpected metadata update: %#v", updated)
	}
	if _, err := store.UpdateMetadata(ctx, UpdateMetadataInput{
		ID:                       item.ID,
		ExpectedMetadataRevision: item.MetadataRevision,
		Name:                     updated.Name,
		OwnerProjectID:           owner.ID,
		SecretType:               updated.SecretType,
		ExpiryWarningDays:        30,
	}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale metadata update error = %v", err)
	}

	if err := store.Delete(ctx, item.ID, item.ValueVersion, item.MetadataRevision); !errors.Is(err, ErrStale) {
		t.Fatalf("stale delete error = %v", err)
	}
	if err := store.Delete(ctx, item.ID, updated.ValueVersion, updated.MetadataRevision); err != nil {
		t.Fatalf("delete vault item: %v", err)
	}
	if _, err := store.Get(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted item get error = %v", err)
	}
}

func TestProjectArchiveIsBlockedByOwnedAndSharedVaultItems(t *testing.T) {
	ctx := context.Background()
	database, store := openTestStore(t)
	projects := projectstore.NewStore(database)
	owner, err := projects.Create(ctx, "Owner")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	shared, err := projects.Create(ctx, "Shared")
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	item, err := store.Create(ctx, CreateInput{
		Name:             "ARCHIVE_GUARD_SECRET",
		Value:            "archive-guard-value",
		OwnerProjectID:   owner.ID,
		SharedProjectIDs: []int64{shared.ID},
		SecretType:       "generic_secret",
		Source:           "imported",
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	if err := projects.Archive(ctx, owner.ID); !errors.Is(err, projectstore.ErrProjectNotEmpty) {
		t.Fatalf("owner archive error = %v", err)
	}
	if err := projects.Archive(ctx, shared.ID); !errors.Is(err, projectstore.ErrProjectNotEmpty) {
		t.Fatalf("shared archive error = %v", err)
	}
	if err := store.Delete(ctx, item.ID, item.ValueVersion, item.MetadataRevision); err != nil {
		t.Fatalf("delete item: %v", err)
	}
	if err := projects.Archive(ctx, shared.ID); err != nil {
		t.Fatalf("archive detached shared project: %v", err)
	}
}

func TestVaultItemAADRejectsRecordAndWorkspaceSubstitution(t *testing.T) {
	ctx := context.Background()
	database, store := openTestStore(t)
	project, err := projectstore.NewStore(database).Create(ctx, "Core Platform")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	first, err := store.Create(ctx, CreateInput{
		Name:           "FIRST_SECRET",
		Value:          "first-value-long-enough",
		OwnerProjectID: project.ID,
		SecretType:     "generic_secret",
		Source:         "imported",
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := store.Create(ctx, CreateInput{
		Name:           "SECOND_SECRET",
		Value:          "second-value-long-enough",
		OwnerProjectID: project.ID,
		SecretType:     "generic_secret",
		Source:         "imported",
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE vault_items
		SET encrypted_value = (SELECT encrypted_value FROM vault_items WHERE id = ?)
		WHERE id = ?`, first.ID, second.ID); err != nil {
		t.Fatalf("substitute ciphertext: %v", err)
	}
	if _, err := store.Reveal(ctx, second.ID); err == nil {
		t.Fatalf("record-substituted ciphertext should fail")
	}

	otherStore, err := NewStore(database, store.vault, "different-workspace-uuid")
	if err != nil {
		t.Fatalf("create other workspace store: %v", err)
	}
	if _, err := otherStore.Reveal(ctx, first.ID); err == nil {
		t.Fatalf("workspace-substituted ciphertext should fail")
	}
}

func TestWorkspaceUUIDIsStableAndGeneratorValuesStayEncrypted(t *testing.T) {
	ctx := context.Background()
	database, store := openTestStore(t)
	firstUUID, err := EnsureWorkspaceUUID(ctx, database)
	if err != nil {
		t.Fatalf("ensure first UUID: %v", err)
	}
	secondUUID, err := EnsureWorkspaceUUID(ctx, database)
	if err != nil {
		t.Fatalf("ensure second UUID: %v", err)
	}
	if firstUUID != secondUUID || firstUUID != store.workspaceUUID {
		t.Fatalf("workspace UUID changed: %q %q %q", firstUUID, secondUUID, store.workspaceUUID)
	}
	project, err := projectstore.NewStore(database).Create(ctx, "Generated")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	item, err := store.Create(ctx, CreateInput{
		Name:           "GENERATED_API_KEY",
		OwnerProjectID: project.ID,
		SecretType:     "api_key",
		Source:         "generated",
		GeneratorKind:  "random_token",
		ExpiresAt:      time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("create generated item: %v", err)
	}
	value, err := store.Reveal(ctx, item.ID)
	if err != nil {
		t.Fatalf("reveal generated item: %v", err)
	}
	if len(value) < 40 || item.GeneratorParameters["encoding"] != "base64url" {
		t.Fatalf("unexpected generated item: value length=%d item=%#v", len(value), item)
	}
}

func TestStoreValidationFailsClosed(t *testing.T) {
	ctx := context.Background()
	database, store := openTestStore(t)
	project, err := projectstore.NewStore(database).Create(ctx, "Validation")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	valid := CreateInput{
		Name:           "VALID_SECRET",
		Value:          "valid-value-long-enough",
		OwnerProjectID: project.ID,
		SecretType:     "generic_secret",
		Source:         "imported",
	}
	cases := []struct {
		name   string
		mutate func(*CreateInput)
	}{
		{name: "lowercase name", mutate: func(input *CreateInput) { input.Name = "invalid" }},
		{name: "digit prefix", mutate: func(input *CreateInput) { input.Name = "1_INVALID" }},
		{name: "reserved exact", mutate: func(input *CreateInput) { input.Name = "PATH" }},
		{name: "reserved prefix", mutate: func(input *CreateInput) { input.Name = "LD_PRELOAD" }},
		{name: "nul value", mutate: func(input *CreateInput) { input.Value = "value\x00secret" }},
		{name: "invalid type", mutate: func(input *CreateInput) { input.SecretType = "certificate" }},
		{name: "expired", mutate: func(input *CreateInput) { input.ExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339) }},
		{name: "missing project", mutate: func(input *CreateInput) { input.OwnerProjectID = 99999 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := store.Create(ctx, input); err == nil {
				t.Fatalf("expected validation failure")
			}
		})
	}
}

func TestGeneratorsUseExpectedShapes(t *testing.T) {
	for _, kind := range []string{"random_token", "hex_secret", "password", "long_hmac_secret", "uuid_v4"} {
		t.Run(kind, func(t *testing.T) {
			value, parameters, err := Generate(kind)
			if err != nil {
				t.Fatalf("generate %s: %v", kind, err)
			}
			if value == "" || len(parameters) == 0 {
				t.Fatalf("empty generated result: %q %#v", value, parameters)
			}
		})
	}
	if _, _, err := Generate("sha256"); err == nil {
		t.Fatalf("unsupported generator should fail")
	}
}

func openTestStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "project-vault.db"), "test-password")
	if err != nil {
		t.Fatalf("open encrypted database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatalf("create secret vault: %v", err)
	}
	workspaceUUID, err := EnsureWorkspaceUUID(context.Background(), database)
	if err != nil {
		t.Fatalf("ensure workspace UUID: %v", err)
	}
	store, err := NewStore(database, secretVault, workspaceUUID)
	if err != nil {
		t.Fatalf("create project vault store: %v", err)
	}
	return database, store
}
