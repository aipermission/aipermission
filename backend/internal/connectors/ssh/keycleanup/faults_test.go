package keycleanup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestJournalCreateFailuresNeverPermitDispatch(t *testing.T) {
	ctx := context.Background()
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before commit", true: "after commit"}[after], func(t *testing.T) {
			store := newMemoryStore()
			store.createErr, store.createAfter = errors.New("create fault"), after
			journal := New(store)
			identity := testIdentity(t)
			if _, dispatch, err := journal.Begin(ctx, identity); err == nil || dispatch {
				t.Fatalf("failed create authorized dispatch: %t, %v", dispatch, err)
			}
			store.createErr = nil
			_, dispatch, err := New(store).Begin(ctx, identity)
			if after && (dispatch || !errors.Is(err, ErrReconciliationRequired)) {
				t.Fatalf("ambiguous create lost its fence: %t, %v", dispatch, err)
			}
			if !after && (err != nil || !dispatch) {
				t.Fatalf("no intent persisted but fresh begin failed: %t, %v", dispatch, err)
			}
		})
	}
}

func TestJournalConfirmationFaultsCannotAuthorizeAnotherAuthentication(t *testing.T) {
	ctx := context.Background()
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before commit", true: "after commit"}[after], func(t *testing.T) {
			store := newMemoryStore()
			journal := New(store)
			identity := testIdentity(t)
			intent := beginTest(t, journal, identity)
			store.updateErr, store.updateAfter = errors.New("update fault"), after
			if _, err := journal.Confirm(ctx, intent); err == nil {
				t.Fatal("failed update reported confirmed")
			}
			store.updateErr = nil
			_, dispatch, err := New(store).Begin(ctx, identity)
			if dispatch || (!after && !errors.Is(err, ErrReconciliationRequired)) || (after && err != nil) {
				t.Fatalf("unsafe confirmation recovery: dispatch %t, %v", dispatch, err)
			}
		})
	}
}

func TestJournalRejectsMismatchedPersistenceReadback(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	journal := New(store)
	identity := testIdentity(t)
	store.readback = func(row resourcecontract.CredentialResource) resourcecontract.CredentialResource {
		row.PublicData = strings.Replace(row.PublicData, `"status":"intent"`, `"status":"confirmed"`, 1)
		return row
	}
	if _, dispatch, err := journal.Begin(ctx, identity); err == nil || dispatch {
		t.Fatalf("mismatched intent readback authorized dispatch: %t, %v", dispatch, err)
	}
	store.readback = nil
	entries, err := journal.List(ctx)
	if err != nil || len(entries) != 1 || entries[0].Record.Status != Intent {
		t.Fatalf("persisted fence was not retained: %#v, %v", entries, err)
	}
	store.readback = func(row resourcecontract.CredentialResource) resourcecontract.CredentialResource {
		row.ID++
		return row
	}
	if _, err := journal.Confirm(ctx, entries[0]); err == nil {
		t.Fatal("mismatched decision readback reported success")
	}
}

func TestJournalReadErrorsAndUnavailableStoreFailClosed(t *testing.T) {
	ctx := context.Background()
	identity := testIdentity(t)
	for _, journal := range []*Journal{nil, New(nil)} {
		if _, dispatch, err := journal.Begin(ctx, identity); err == nil || dispatch {
			t.Fatalf("unavailable journal authorized dispatch: %t, %v", dispatch, err)
		}
		if _, err := journal.Confirm(ctx, Entry{}); err == nil {
			t.Fatal("unavailable confirmation succeeded")
		}
	}
	store := newMemoryStore()
	journal := New(store)
	store.listErr = errors.New("list fault")
	if _, dispatch, err := journal.Begin(ctx, identity); err == nil || dispatch {
		t.Fatalf("failed journal read authorized dispatch: %t, %v", dispatch, err)
	}
	store.listErr = nil
	intent := beginTest(t, journal, identity)
	store.getErr = errors.New("get fault")
	if _, err := journal.Confirm(ctx, intent); err == nil {
		t.Fatal("failed generation read succeeded")
	}
}

func TestJournalMalformedOrDuplicatedRecordsFailClosed(t *testing.T) {
	mutations := map[string]func(*resourcecontract.CredentialResource){
		"json":     func(r *resourcecontract.CredentialResource) { r.PublicData = "{" },
		"trailing": func(r *resourcecontract.CredentialResource) { r.PublicData += " {}" },
		"unknown field": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"version":2`, `"version":2,"unknown":true`, 1)
		},
		"duplicate field": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"version":2`, `"version":2,"version":2`, 1)
		},
		"version": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"version":2`, `"version":1`, 1)
		},
		"status": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"status":"intent"`, `"status":"success"`, 1)
		},
		"generation": func(r *resourcecontract.CredentialResource) {
			var record Record
			_ = json.Unmarshal([]byte(r.PublicData), &record)
			record.Generation = "bad"
			data, _ := json.Marshal(record)
			r.PublicData = string(data)
		},
		"name":        func(r *resourcecontract.CredentialResource) { r.Name += "other" },
		"fingerprint": func(r *resourcecontract.CredentialResource) { r.Fingerprint = "other" },
		"kind":        func(r *resourcecontract.CredentialResource) { r.ResourceType = "other" },
		"id":          func(r *resourcecontract.CredentialResource) { r.ID = 0 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			store := newMemoryStore()
			journal := New(store)
			identity := testIdentity(t)
			entry := beginTest(t, journal, identity)
			row := store.rows[entry.ResourceID]
			mutate(&row)
			store.rows[entry.ResourceID] = row
			if _, dispatch, err := journal.Begin(context.Background(), identity); err == nil || dispatch {
				t.Fatalf("invalid journal authorized dispatch: %t, %v", dispatch, err)
			}
		})
	}
	store := newMemoryStore()
	journal := New(store)
	identity := testIdentity(t)
	entry := beginTest(t, journal, identity)
	store.rows[entry.ResourceID+1] = store.rows[entry.ResourceID]
	if _, dispatch, err := journal.Begin(context.Background(), identity); err == nil || dispatch || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate journal authorized dispatch: %t, %v", dispatch, err)
	}
}
