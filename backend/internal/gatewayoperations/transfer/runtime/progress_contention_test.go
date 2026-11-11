package transferruntime

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTransferProgressDoesNotWaitIndefinitelyForDatabase(t *testing.T) {
	runner, runtime, batch, database := newNativeDownloadBudgetFixture(t, 1)
	id := batch.Items[0].ID
	if _, err := runtime.store.MarkRunning(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	closeConnection := func() { release.Do(func() { _ = connection.Close() }) }
	t.Cleanup(closeConnection)
	progress := runner.transferProgress(t.Context(), runtime, id)
	done := make(chan struct{})
	go func() {
		defer close(done)
		progress(4, 8)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		closeConnection()
		<-done
		t.Fatal("best-effort progress blocked the transfer on the database connection")
	}
	closeConnection()
	progress(8, 8)
	item, err := runtime.store.Get(t.Context(), id)
	if err != nil || item.TransferredBytes != 8 {
		t.Fatalf("progress did not recover after contention: bytes=%d err=%v", item.TransferredBytes, err)
	}
}

func TestTransferProgressDoesNotWriteAfterOwnerCancellation(t *testing.T) {
	runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 1)
	id := batch.Items[0].ID
	if _, err := runtime.store.MarkRunning(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runner.transferProgress(ctx, runtime, id)(4, 8)
	item, err := runtime.store.Get(t.Context(), id)
	if err != nil || item.TransferredBytes != 0 {
		t.Fatalf("progress wrote after owner canceled: bytes=%d err=%v", item.TransferredBytes, err)
	}
}
