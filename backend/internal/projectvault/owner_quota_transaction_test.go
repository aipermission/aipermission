package projectvault

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestMetadataStaleTransferPrecedesFullOwnerQuota(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, 1)
	updated := populateQuotaRelations(t, store, item, destination)
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner)
	_, err := store.UpdateMetadata(t.Context(), quotaMetadataInput(item, destination))
	if !errors.Is(err, ErrStale) {
		t.Fatalf("full destination masked stale revision: %v", err)
	}
	assertQuotaItemUnchanged(t, store, updated)
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
}

type quotaCancelAfterBegin struct {
	*sql.DB
	cancel context.CancelFunc
	began  bool
}

func (database *quotaCancelAfterBegin) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	tx, err := database.DB.BeginTx(ctx, options)
	if err == nil {
		database.began = true
		database.cancel()
	}
	return tx, err
}

func TestMetadataOwnerTransferCancellationAfterBeginPreservesItem(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, 1)
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := &quotaCancelAfterBegin{DB: database, cancel: cancel}
	withCancel := *store
	withCancel.db = fixture
	_, err := withCancel.UpdateMetadata(ctx, quotaMetadataInput(item, destination))
	if !fixture.began || ctx.Err() != context.Canceled || (!errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone)) {
		t.Fatalf("transaction cancellation: began=%t context=%v error=%v", fixture.began, ctx.Err(), err)
	}
	assertQuotaItemUnchanged(t, store, item)
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
}

func TestMetadataOwnerTransferRetainsOuterTransactionOwnership(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, 1)
	item = populateQuotaRelations(t, store, item, destination)
	relations := quotaRelationSnapshot(t, database, item.ID)
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner-1)
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	updated, err := store.WithTx(tx).UpdateMetadata(t.Context(), quotaMetadataInput(item, destination))
	if err != nil || updated.OwnerProjectID != destination {
		t.Fatalf("outer transaction transfer: %#v %v", updated, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("metadata update committed the caller transaction: %v", err)
	}
	assertQuotaItemUnchanged(t, store, item)
	assertQuotaRelations(t, database, item.ID, relations)
	assertOwnerCount(t, database, source, 1)
	assertOwnerCount(t, database, destination, maxItemsPerOwner-1)
}
