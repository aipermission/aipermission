package keycleanup

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAttestationRejectsIncompleteOrUnboundExternalEvidence(t *testing.T) {
	mutations := map[string]func(*Attestation){
		"only one endpoint":   func(p *Attestation) { p.Coverage = p.Coverage[:1] },
		"no coverage":         func(p *Attestation) { p.Coverage = nil },
		"duplicate subject":   func(p *Attestation) { p.Coverage[1] = p.Coverage[0] },
		"unknown subject":     func(p *Attestation) { p.Coverage[0].SubjectID = testDigest(t, "unrelated") },
		"unsorted subjects":   func(p *Attestation) { slices.Reverse(p.Coverage) },
		"absence unchecked":   func(p *Attestation) { p.Coverage[0].Absent = false },
		"untrusted method":    func(p *Attestation) { p.Coverage[0].Method = "same_connector_retry" },
		"missing method":      func(p *Attestation) { p.Coverage[0].Method = "" },
		"missing explanation": func(p *Attestation) { p.Coverage[0].Reason = "" },
		"blank explanation":   func(p *Attestation) { p.Coverage[0].Reason = "   " },
		"padded explanation":  func(p *Attestation) { p.Coverage[0].Reason = " padded " },
		"long explanation":    func(p *Attestation) { p.Coverage[0].Reason = strings.Repeat("x", 2001) },
		"missing context":     func(p *Attestation) { p.ContextDigest = "" },
		"malformed context":   func(p *Attestation) { p.ContextDigest = strings.Repeat("Z", 64) },
		"invalid identity":    func(p *Attestation) { p.Identity.Port = 0 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			store := newMemoryStore()
			journal := New(store)
			entry := beginTest(t, journal, testIdentity(t))
			current := testIdentity(t)
			current.Host = "replacement.test"
			decision := decisionForTest(t, entry, current, "Verified both independent locations")
			mutate(&decision)
			if _, err := journal.Attest(t.Context(), entry, decision); err == nil {
				t.Fatal("invalid evidence resolved historical uncertainty")
			}
			entries, err := journal.List(t.Context())
			if err != nil || len(entries) != 1 || !reflect.DeepEqual(entries[0], entry) || store.secretReads != 0 {
				t.Fatalf("invalid evidence mutated or disclosed state: %#v %v", entries, err)
			}
		})
	}
}

func TestPersistedAttestationCannotOmitEarlierLocationCoverage(t *testing.T) {
	for _, corrupt := range []string{"missing", "duplicate", "unknown", "method", "context"} {
		t.Run(corrupt, func(t *testing.T) {
			store := newMemoryStore()
			journal := New(store)
			entry := beginTest(t, journal, testIdentity(t))
			current := testIdentity(t)
			current.Host = "replacement.test"
			result, err := journal.Attest(t.Context(), entry, decisionForTest(t, entry, current, "All locations verified"))
			if err != nil {
				t.Fatal(err)
			}
			switch corrupt {
			case "missing":
				result.Record.Attestations[0].Coverage = result.Record.Attestations[0].Coverage[:1]
			case "duplicate":
				result.Record.Attestations[0].Coverage[1] = result.Record.Attestations[0].Coverage[0]
			case "unknown":
				result.Record.Attestations[0].Coverage[0].SubjectID = testDigest(t, "unknown")
			case "method":
				result.Record.Attestations[0].Coverage[0].Method = "automatic_retry"
			case "context":
				result.Record.Attestations[0].ContextDigest = ""
			}
			delete(store.rows, result.ResourceID)
			seedTestEntry(t, store, result.Record)
			if _, dispatch, err := journal.Begin(t.Context(), current); err == nil || dispatch {
				t.Fatalf("corrupt persisted evidence authorized dispatch: %t %v", dispatch, err)
			}
		})
	}
}
