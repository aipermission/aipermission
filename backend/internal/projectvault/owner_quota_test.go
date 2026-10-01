package projectvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"

	appdb "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func TestMetadataOwnerTransferRejectsFullProjectWithoutChangingItem(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, 1)
	item = populateQuotaRelations(t, store, item, destination)
	relations := quotaRelationSnapshot(t, database, item.ID)
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner)
	input := quotaMetadataInput(item, destination)
	input.Name = "SHOULD_NOT_CHANGE"
	input.Tags = []string{"new-tag"}
	input.SharedProjectIDs = []int64{source}
	_, err := store.UpdateMetadata(t.Context(), input)
	assertProjectQuotaError(t, err)
	assertQuotaItemUnchanged(t, store, item)
	assertQuotaRelations(t, database, item.ID, relations)
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
	assertOwnerCount(t, database, source, 1)
	store, database = reopenQuotaStore(t, database, store)
	assertQuotaItemUnchanged(t, store, item)
	assertQuotaRelations(t, database, item.ID, relations)
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
}

func TestMetadataEditsAtOwnerLimitKeepStaleAndMissingSemantics(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, destination, maxItemsPerOwner)
	input := quotaMetadataInput(item, destination)
	input.Description = "Metadata edits do not consume another slot."
	updated, err := store.UpdateMetadata(t.Context(), input)
	if err != nil || updated.MetadataRevision != item.MetadataRevision+1 || updated.ValueVersion != item.ValueVersion {
		t.Fatalf("same-owner edit: item=%#v error=%v", updated, err)
	}
	_, err = store.UpdateMetadata(t.Context(), input)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("stale edit returned %v", err)
	}
	input.OwnerProjectID = source
	_, err = store.UpdateMetadata(t.Context(), input)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("stale transfer returned %v", err)
	}
	input.ID = item.ID + maxItemsPerDatabase
	_, err = store.UpdateMetadata(t.Context(), input)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing edit returned %v", err)
	}
	_, err = store.Create(t.Context(), CreateInput{
		Name: "EXCESS_ITEM", Value: "quota-fixture-value", OwnerProjectID: destination,
		SecretType: DefaultSecretType, Source: "imported",
	})
	assertProjectQuotaError(t, err)
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
	assertQuotaItemUnchanged(t, store, updated)
}

func TestMetadataOwnerTransferSerializesLastSlot(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	first := seedQuotaItems(t, database, store, source, 1)
	second, err := store.Create(t.Context(), CreateInput{
		Name: "SECOND_SOURCE_ITEM", Value: "quota-fixture-value", OwnerProjectID: source,
		SecretType: DefaultSecretType, Source: "imported",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner-1)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, item := range []Item{first, second} {
		go func() {
			<-start
			_, err := store.UpdateMetadata(t.Context(), quotaMetadataInput(item, destination))
			results <- err
		}()
	}
	close(start)
	completed := []error{<-results, <-results}
	successes := 0
	for _, err := range completed {
		if err == nil {
			successes++
		} else {
			assertProjectQuotaError(t, err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful transfers=%d, want one", successes)
	}
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
	assertOwnerCount(t, database, source, 1)
	for _, item := range []Item{first, second} {
		value, err := store.Reveal(t.Context(), item.ID)
		if err != nil || value != "quota-fixture-value" {
			t.Fatalf("transferred item value=%q error=%v", value, err)
		}
	}
}

func TestMetadataOwnerTransferCountsActiveExpiredButNotArchivedItems(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, 1)
	destinationItem := seedQuotaItems(t, database, store, destination, maxItemsPerOwner)
	if _, err := database.Exec(`UPDATE vault_items SET expires_at = '2000-01-01T00:00:00Z' WHERE id = ?`, destinationItem.ID); err != nil {
		t.Fatal(err)
	}
	_, err := store.UpdateMetadata(t.Context(), quotaMetadataInput(item, destination))
	assertProjectQuotaError(t, err)
	assertQuotaItemUnchanged(t, store, item)
	if _, err := database.Exec(`UPDATE vault_items SET status = 'archived' WHERE id = ?`, destinationItem.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateMetadata(t.Context(), quotaMetadataInput(item, destination))
	if err != nil || updated.OwnerProjectID != destination {
		t.Fatalf("archived item consumed a quota slot: %#v %v", updated, err)
	}
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
}

func TestMetadataOwnerTransferRollsBackSlotAndRelations(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, 1)
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner-1)
	item = populateQuotaRelations(t, store, item, destination)
	relations := quotaRelationSnapshot(t, database, item.ID)
	if _, err := database.Exec(`CREATE TRIGGER fail_quota_notes BEFORE INSERT ON vault_item_usage_notes
		BEGIN SELECT RAISE(ABORT, 'quota fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	input := quotaMetadataInput(item, destination)
	input.Tags = []string{"new-tag"}
	input.SharedProjectIDs = []int64{source}
	input.UsageNotes = []UsageNote{{Location: "new-location", Notes: "new-notes"}}
	if _, err := store.UpdateMetadata(t.Context(), input); err == nil {
		t.Fatal("expected usage-note persistence to abort the transaction")
	}
	assertQuotaItemUnchanged(t, store, item)
	assertQuotaRelations(t, database, item.ID, relations)
	assertOwnerCount(t, database, source, 1)
	assertOwnerCount(t, database, destination, maxItemsPerOwner-1)
	if _, err := database.Exec(`DROP TRIGGER fail_quota_notes`); err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateMetadata(t.Context(), input)
	if err != nil || updated.OwnerProjectID != destination || updated.MetadataRevision != item.MetadataRevision+1 {
		t.Fatalf("retry after rollback: item=%#v error=%v", updated, err)
	}
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
	assertOwnerCount(t, database, source, 0)
	store, database = reopenQuotaStore(t, database, store)
	assertQuotaItemUnchanged(t, store, updated)
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
}

func TestMetadataOwnerTransferCancellationPreservesItem(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, 1)
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.UpdateMetadata(ctx, quotaMetadataInput(item, destination)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled transfer=%v", err)
	}
	assertQuotaItemUnchanged(t, store, item)
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
}

func quotaProjects(t *testing.T, database *sql.DB) (int64, int64) {
	t.Helper()
	projects := projectstore.NewStore(database)
	source, err := projects.Create(t.Context(), "Source Project")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := projects.Create(t.Context(), "Destination Project")
	if err != nil {
		t.Fatal(err)
	}
	return source.ID, destination.ID
}

func seedQuotaItems(t *testing.T, database *sql.DB, store *Store, owner int64, count int) Item {
	t.Helper()
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var first Item
	for index := range count {
		item, err := store.WithTx(tx).Create(t.Context(), CreateInput{
			Name: fmt.Sprintf("QUOTA_%d_%d", owner, index), Value: "quota-fixture-value", OwnerProjectID: owner,
			SecretType: DefaultSecretType, Source: "imported",
		})
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = item
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return first
}

func quotaMetadataInput(item Item, owner int64) UpdateMetadataInput {
	return UpdateMetadataInput{
		ID: item.ID, ExpectedMetadataRevision: item.MetadataRevision, Name: item.Name,
		OwnerProjectID: owner, SecretType: item.SecretType, ExpiryWarningDays: item.ExpiryWarningDays,
	}
}

func assertProjectQuotaError(t *testing.T, err error) {
	t.Helper()
	var validation ValidationError
	if !errors.As(err, &validation) || validation != "vault item project quota reached" {
		t.Fatalf("expected project quota error, got %v", err)
	}
}

func assertQuotaItemUnchanged(t *testing.T, store *Store, expected Item) {
	t.Helper()
	item, err := store.Get(t.Context(), expected.ID)
	if err != nil || !reflect.DeepEqual(item, expected) {
		t.Fatalf("item changed: %#v error=%v, want %#v", item, err, expected)
	}
	if value, err := store.Reveal(t.Context(), expected.ID); err != nil || value != "quota-fixture-value" {
		t.Fatalf("value=%q error=%v", value, err)
	}
}

func assertOwnerCount(t *testing.T, database *sql.DB, owner int64, want int) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM vault_items WHERE owner_project_id = ? AND status = 'active'`, owner).Scan(&count); err != nil || count != want {
		t.Fatalf("owner count=%d error=%v, want %d", count, err, want)
	}
}

func reopenQuotaStore(t *testing.T, database *sql.DB, store *Store) (*Store, *sql.DB) {
	t.Helper()
	var sequence int
	var name, path string
	if err := database.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := appdb.OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	result, err := NewStore(reopened, store.vault, store.workspaceUUID)
	if err != nil {
		t.Fatal(err)
	}
	return result, reopened
}
