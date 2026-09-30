package keycleanup

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestJournalAttestationRetainsAllHistoricalAliasFences(t *testing.T) {
	ctx := context.Background()
	journal := New(newMemoryStore())
	original := testIdentity(t)
	intent := beginTest(t, journal, original)
	changed := testIdentity(t)
	changed.Host, changed.HostFingerprints = "new.test", []string{testFingerprint("new-host")}
	attested, err := journal.Attest(ctx, intent, changed, "Verified absence on the replacement endpoint")
	if err != nil {
		t.Fatal(err)
	}
	alias := testIdentity(t)
	alias.TargetID, alias.Profiles[0].ID, alias.Profiles[0].KeyID = 10, 11, 12
	alias.Host, alias.HostFingerprints = changed.Host, changed.HostFingerprints
	if _, dispatch, err := journal.Begin(ctx, alias); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("attested endpoint alias escaped historical fence: %t, %v", dispatch, err)
	}
	aliasAttested, err := journal.Attest(ctx, attested, alias, "Verified absence after importing the same key")
	if err != nil || len(aliasAttested.Record.Attestations) != 2 {
		t.Fatalf("chained attestation = %#v, %v", aliasAttested, err)
	}
	if !reflect.DeepEqual(aliasAttested.Record.Identity, intent.Record.Identity) ||
		!reflect.DeepEqual(aliasAttested.Record.Attestations[:1], attested.Record.Attestations) {
		t.Fatal("chained attestation rewrote original identity or previous evidence")
	}
	if _, dispatch, err := journal.Begin(ctx, alias); err != nil || dispatch {
		t.Fatalf("latest externally verified alias = %t, %v", dispatch, err)
	}
	for _, historical := range []Identity{original, changed} {
		if _, dispatch, err := journal.Begin(ctx, historical); dispatch || !errors.Is(err, ErrReconciliationRequired) {
			t.Fatalf("older identity lost its fence: %t, %v", dispatch, err)
		}
	}
	alias.TargetRevision = "new generation"
	if _, dispatch, err := journal.Begin(ctx, alias); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("latest alias drift escaped fence: %t, %v", dispatch, err)
	}
}

func TestJournalEveryOverlappingHistoryMustBeResolvedInEitherListOrder(t *testing.T) {
	ctx := context.Background()
	for _, reversed := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed first", true: "intent first"}[reversed], func(t *testing.T) {
			store := newMemoryStore()
			journal := New(store)
			identity := testIdentity(t)
			first := seedTestEntry(t, store, Record{Identity: identity, Status: Confirmed})
			other := testIdentity(t)
			other.Profiles[0].ID++
			second := seedTestEntry(t, store, Record{Identity: other, Status: Intent})
			store.listOrder = []int64{first.ResourceID, second.ResourceID}
			if reversed {
				store.listOrder = []int64{second.ResourceID, first.ResourceID}
			}
			if _, dispatch, err := journal.Begin(ctx, identity); dispatch || !errors.Is(err, ErrReconciliationRequired) {
				t.Fatalf("one resolved history masked unresolved intent: %t, %v", dispatch, err)
			}
			if _, err := journal.Attest(ctx, second, identity, "Both histories externally verified for this exact snapshot"); err != nil {
				t.Fatal(err)
			}
			if _, dispatch, err := journal.Begin(ctx, identity); err != nil || dispatch || len(store.rows) != 2 {
				t.Fatalf("resolved combined history = %t, %v, rows %d", dispatch, err, len(store.rows))
			}
		})
	}
}

func TestJournalLostAttestationResponsesKeepAuthenticationSuppressed(t *testing.T) {
	ctx := context.Background()
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before commit", true: "after commit"}[after], func(t *testing.T) {
			store := newMemoryStore()
			journal := New(store)
			identity := testIdentity(t)
			intent := beginTest(t, journal, identity)
			changed := testIdentity(t)
			changed.Profiles[0].KeyID++
			store.updateErr, store.updateAfter = errors.New("lost attestation response"), after
			if _, err := journal.Attest(ctx, intent, changed, "Externally verified absence"); err == nil {
				t.Fatal("lost response was reported successful")
			}
			store.updateErr = nil
			_, dispatch, err := New(store).Begin(ctx, changed)
			if dispatch || (after && err != nil) || (!after && !errors.Is(err, ErrReconciliationRequired)) {
				t.Fatalf("lost attestation response authorized authentication: %t, %v", dispatch, err)
			}
		})
	}
}

func TestJournalRejectsCanonicalButInvalidAttestationData(t *testing.T) {
	identity := testIdentity(t)
	mutations := map[string]func(*Record){
		"missing snapshots":  func(r *Record) { r.Attestations = nil },
		"blank reason":       func(r *Record) { r.Attestations[0].Reason = "   " },
		"padded reason":      func(r *Record) { r.Attestations[0].Reason = " padded " },
		"invalid identity":   func(r *Record) { r.Attestations[0].Identity.Host = "" },
		"unrelated identity": func(r *Record) { r.Attestations[0].Identity.KeyDigest = testDigest(t, "unrelated") },
		"disconnected middle proof": func(r *Record) {
			middle := testIdentity(t)
			middle.KeyDigest = testDigest(t, "unrelated")
			r.Attestations = append(r.Attestations, Attestation{Identity: middle, Reason: "Unrelated evidence"}, r.Attestations[0])
		},
		"intent with attestation": func(r *Record) { r.Status = Intent },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			store := newMemoryStore()
			record := Record{Identity: identity, Status: Attested, Attestations: []Attestation{{Identity: testIdentity(t), Reason: "External verification"}}}
			mutate(&record)
			seedTestEntry(t, store, record)
			if _, dispatch, err := New(store).Begin(context.Background(), identity); err == nil || dispatch {
				t.Fatalf("canonical invalid attestation authorized dispatch: %t, %v", dispatch, err)
			}
		})
	}
	store := newMemoryStore()
	intent := beginTest(t, New(store), identity)
	store.readback = func(row connectorapi.CredentialResource) connectorapi.CredentialResource {
		var record Record
		_ = json.Unmarshal([]byte(row.PublicData), &record)
		record.Attestations[0].Reason = "Different evidence"
		encoded, _ := json.Marshal(record)
		row.PublicData = string(encoded)
		return row
	}
	if _, err := New(store).Attest(context.Background(), intent, identity, "External verification"); err == nil {
		t.Fatal("mismatched attestation readback reported success")
	}
}
