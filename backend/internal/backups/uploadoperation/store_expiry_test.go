package uploadoperation_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups/uploadoperation"
)

func TestUploadJournalExpiryIsScopedAndTerminal(t *testing.T) {
	database, store, request := journalClaim(t)
	if _, err := database.ExecContext(t.Context(), `INSERT INTO backup_providers (id, provider_type, name, created_at, updated_at) VALUES (?, 'aipermission_backup', 'Other provider', '', '')`, request.ProviderID+1); err != nil {
		t.Fatal(err)
	}
	states := []string{"pending", "dispatched", "outcome_unknown", "completed", "expired", "foreign"}
	before := make(map[string]uploadoperation.Operation)
	for _, state := range states {
		r := request
		r.IdempotencyKey = state
		if state == "foreign" {
			r.ProviderID++
		}
		if _, _, err := store.Claim(t.Context(), r); err != nil {
			t.Fatal(err)
		}
		var err error
		switch state {
		case "dispatched":
			err = store.MarkDispatched(t.Context(), state)
		case "outcome_unknown":
			err = store.MarkOutcomeUnknown(t.Context(), state, nil)
		case "completed":
			err = store.Complete(t.Context(), state, "result")
		case "expired":
			err = store.MarkExpired(t.Context(), state)
		}
		if err != nil {
			t.Fatal(err)
		}
		before[state], err = store.Get(t.Context(), state)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ExpireUnresolved(t.Context(), request.ProviderID, "identity changed"); err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		after, err := store.Get(t.Context(), state)
		if err != nil {
			t.Fatal(err)
		}
		switch state {
		case "pending", "dispatched", "outcome_unknown":
			if after.Status != "expired" || after.CompletedAt == nil || after.LastError != "identity changed" {
				t.Fatalf("expiry did not settle %s: %#v", state, after)
			}
		default:
			if !reflect.DeepEqual(before[state], after) {
				t.Fatalf("expiry changed unrelated/terminal %s: %#v", state, after)
			}
		}
	}
	if err := store.MarkDispatched(t.Context(), "expired"); err == nil {
		t.Fatal("expired operation was reopened")
	}
	if err := store.Complete(t.Context(), "expired", "result"); err == nil {
		t.Fatal("expired operation was completed")
	}
	if err := store.ExpireResult(t.Context(), request.ProviderID+1, "result", "now"); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.Get(t.Context(), "completed")
	if err != nil || !reflect.DeepEqual(before["completed"], unchanged) {
		t.Fatalf("foreign result expiry: %#v err=%v", unchanged, err)
	}
	if err := store.ExpireResult(t.Context(), request.ProviderID, "result", "now"); err != nil {
		t.Fatal(err)
	}
	expired, err := store.Get(t.Context(), "completed")
	if err != nil || expired.Status != "expired" || expired.ProviderFileID != "result" || expired.CompletedAt == nil || *expired.CompletedAt != *unchanged.CompletedAt {
		t.Fatalf("remote deletion expiry: %#v err=%v", expired, err)
	}
}

func TestUploadJournalValidationAndMissingTransitions(t *testing.T) {
	_, store, request := journalClaim(t)
	request.IdempotencyKey = "invalid/key"
	_, _, err := store.Claim(t.Context(), request)
	var validation uploadoperation.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("invalid claim error: %v", err)
	}
	if err := store.Complete(t.Context(), "", ""); !errors.As(err, &validation) {
		t.Fatalf("invalid completion error: %v", err)
	}
	if err := store.MarkDispatched(t.Context(), "missing"); err == nil {
		t.Fatal("missing operation dispatched")
	}
	if err := store.Complete(t.Context(), "missing", "result"); err == nil {
		t.Fatal("missing operation completed")
	}
}
