package backups_test

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups"
)

func TestServiceBaselineIsScopedByServiceAndStream(t *testing.T) {
	database := openStoreDatabase(t)
	ctx := context.Background()
	backup := backups.ServiceBackup{ID: "bkp_123", CreatedAt: "2026-07-31T12:00:00Z"}
	if err := backups.WriteServiceBaseline(ctx, database, "https://backup.example.com", "stream-a", backup); err != nil {
		t.Fatal(err)
	}
	baseline, err := backups.ReadServiceBaseline(ctx, database, "https://backup.example.com/", "stream-a")
	if err != nil || baseline == nil || baseline.BackupID != backup.ID || baseline.CreatedAt != backup.CreatedAt {
		t.Fatalf("unexpected baseline: %#v err=%v", baseline, err)
	}
	other, err := backups.ReadServiceBaseline(ctx, database, "https://backup.example.com", "stream-b")
	if err != nil || other != nil {
		t.Fatalf("baseline leaked across streams: %#v err=%v", other, err)
	}
}
