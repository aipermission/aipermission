package projectvault

import (
	"errors"
	"fmt"
	"testing"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func TestMetadataOwnerTransferDoesNotConsumeDatabaseQuota(t *testing.T) {
	database, store := openTestStore(t)
	source, destination := quotaProjects(t, database)
	item := seedQuotaItems(t, database, store, source, maxItemsPerOwner)
	seedQuotaItems(t, database, store, destination, maxItemsPerOwner-1)
	projects := projectstore.NewStore(database)
	remaining := maxItemsPerDatabase - (2*maxItemsPerOwner - 1)
	for index := 0; remaining > 0; index++ {
		project, err := projects.Create(t.Context(), fmt.Sprintf("Additional Project %d", index))
		if err != nil {
			t.Fatal(err)
		}
		count := min(remaining, maxItemsPerOwner)
		seedQuotaItems(t, database, store, project.ID, count)
		remaining -= count
	}
	updated, err := store.UpdateMetadata(t.Context(), quotaMetadataInput(item, destination))
	if err != nil || updated.OwnerProjectID != destination {
		t.Fatalf("same-database transfer at capacity: %#v %v", updated, err)
	}
	assertOwnerCount(t, database, destination, maxItemsPerOwner)
	assertOwnerCount(t, database, source, maxItemsPerOwner-1)
	var total int
	if err := database.QueryRow(`SELECT COUNT(*) FROM vault_items WHERE status = 'active'`).Scan(&total); err != nil || total != maxItemsPerDatabase {
		t.Fatalf("database count=%d error=%v, want %d", total, err, maxItemsPerDatabase)
	}
	_, err = store.Create(t.Context(), CreateInput{Name: "DATABASE_EXCESS", Value: "quota-fixture-value", OwnerProjectID: source,
		SecretType: DefaultSecretType, Source: "imported"})
	var validation ValidationError
	if !errors.As(err, &validation) || validation != "vault item database quota reached" {
		t.Fatalf("create at database capacity returned %v", err)
	}
}
