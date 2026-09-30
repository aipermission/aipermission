package keycleanup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLaterCompleteEvidenceCannotRepairAnIncompleteEarlierProof(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	entry := beginTest(t, journal, testIdentity(t))
	current := testIdentity(t)
	current.Host = "second.test"
	first, err := journal.Attest(t.Context(), entry, decisionForTest(t, entry, current, "Verified both locations"))
	if err != nil {
		t.Fatal(err)
	}
	current.Host = "third.test"
	result, err := journal.Attest(t.Context(), first, decisionForTest(t, first, current, "Verified all three locations"))
	if err != nil || len(result.Record.Attestations[1].Coverage) != 3 {
		t.Fatalf("complete later proof: %#v %v", result, err)
	}
	result.Record.Attestations[0].Coverage = result.Record.Attestations[0].Coverage[:1]
	delete(store.rows, result.ResourceID)
	seedTestEntry(t, store, result.Record)
	if _, dispatch, err := journal.Begin(t.Context(), current); err == nil || dispatch {
		t.Fatalf("later evidence masked incomplete prior proof: %t %v", dispatch, err)
	}
}

func TestEvidenceJSONMustBeCanonicalAtEveryNestedLevel(t *testing.T) {
	mutations := map[string]func(string) string{
		"duplicate absent": func(s string) string { return strings.Replace(s, `"absent":true`, `"absent":false,"absent":true`, 1) },
		"duplicate subject": func(s string) string {
			return strings.Replace(s, `"subject_id":`, `"subject_id":"unrelated","subject_id":`, 1)
		},
		"unknown coverage field": func(s string) string { return strings.Replace(s, `"absent":true`, `"absent":true,"unknown":true`, 1) },
		"null coverage":          func(s string) string { return strings.Replace(s, `"coverage":[`, `"coverage":null,"coverage":[`, 1) },
		"null evidence":          func(s string) string { return strings.Replace(s, `"coverage":[`, `"coverage":[null,`, 1) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			store := newMemoryStore()
			entry := seedTestEntry(t, store, attestedRecordForTest(t, testIdentity(t)))
			row := store.rows[entry.ResourceID]
			data := mutate(row.PublicData)
			if data == row.PublicData || !json.Valid([]byte(data)) {
				t.Fatal("mutation did not create valid, changed JSON")
			}
			row.PublicData = data
			store.rows[entry.ResourceID] = row
			if _, dispatch, err := New(store).Begin(t.Context(), testIdentity(t)); err == nil || dispatch {
				t.Fatalf("ambiguous evidence JSON authorized dispatch: %t %v", dispatch, err)
			}
		})
	}
}

func TestV1ResourcesCannotBeInterpretedAsCompleteHistoricalEvidence(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	entry := seedTestEntry(t, store, attestedRecordForTest(t, testIdentity(t)))
	row := store.rows[entry.ResourceID]
	row.ResourceType = "key_revocation.v1"
	row.PublicData = strings.Replace(row.PublicData, `"version":2`, `"version":1`, 1)
	store.rows[entry.ResourceID] = row
	if _, err := journal.List(t.Context()); err == nil {
		t.Fatal("v1 evidence was silently upgraded")
	}
	if err := journal.RequireTargetHistoryResolved(t.Context(), 1); err == nil {
		t.Fatal("v1 evidence authorized target deletion")
	}
	if store.rows[entry.ResourceID].PublicData != row.PublicData || store.secretReads != 0 {
		t.Fatal("rejected historical evidence was rewritten or disclosed")
	}
}

func TestPersistedAttestationRejectsStandaloneNullCoverage(t *testing.T) {
	record := attestedRecordForTest(t, testIdentity(t))
	record.Attestations[0].Coverage = nil
	store := newMemoryStore()
	seedTestEntry(t, store, record)
	if _, err := New(store).List(t.Context()); err == nil || !strings.Contains(err.Error(), "every historical location") {
		t.Fatalf("standalone null coverage did not fail the coverage invariant: %v", err)
	}
}
