package rolejournal

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type lostUpdateResponse struct {
	resourcecontract.CredentialResourceStore
}

var errLostUpdateResponse = errors.New("lost acknowledgement after durable transition")

func (store lostUpdateResponse) Update(ctx context.Context, id int64, input resourcecontract.UpdateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if _, err := store.CredentialResourceStore.Update(ctx, id, input); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	return resourcecontract.CredentialResource{}, errLostUpdateResponse
}

func TestRoleJournalSQLCipherLostTransitionAcknowledgementsSurviveReopen(t *testing.T) {
	for _, step := range journalTransitions() {
		t.Run(step.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "journal.db")
			database, resources := openPersistedJournal(t, path)
			store := resources.Scope("postgres", ResourceKind)
			entry := step.prepare(t, New(store))
			if got, err := step.run(t.Context(), New(lostUpdateResponse{store}), entry); got.ResourceID != 0 || !errors.Is(err, errLostUpdateResponse) {
				t.Fatalf("lost durable response accepted: %#v %v", got, err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			_, resources = openPersistedJournal(t, path)
			journal := New(resources.Scope("postgres", ResourceKind))
			fresh, err := journal.Get(t.Context(), entry.ResourceID)
			if err != nil {
				t.Fatal(err)
			}
			assertPersistedTransition(t, entry, fresh, step.status)
			if _, err := step.run(t.Context(), journal, entry); !errors.Is(err, ErrStaleGeneration) {
				t.Fatalf("stale transition replayed after reopen: %v", err)
			}
			if step.status == CleanupIntent {
				if _, dispatch, err := journal.BeginCleanup(t.Context(), fresh); dispatch || !errors.Is(err, ErrReconciliationRequired) {
					t.Fatalf("uncertain remote cleanup repeated after reopen: %t %v", dispatch, err)
				}
			}
		})
	}
}
