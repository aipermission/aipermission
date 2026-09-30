package keycleanup

import "testing"

func decisionForTest(t *testing.T, entry Entry, current Identity, reason string) Attestation {
	t.Helper()
	subjects, err := VerificationSubjects(entry.Record, current)
	if err != nil {
		t.Fatal(err)
	}
	decision := Attestation{Identity: current, ContextDigest: testDigest(t, "deletion-context"), Reason: reason}
	for _, subject := range subjects {
		decision.Coverage = append(decision.Coverage, AbsenceEvidence{
			SubjectID: subject.ID, Method: "provider_console", Absent: true,
			Reason: "Fixture operator verified exact key absence at this location",
		})
	}
	return decision
}

func attestedRecordForTest(t *testing.T, identity Identity) Record {
	t.Helper()
	journal := New(newMemoryStore())
	entry := beginTest(t, journal, identity)
	result, err := journal.Attest(t.Context(), entry, decisionForTest(t, entry, identity, "External verification"))
	if err != nil {
		t.Fatal(err)
	}
	return result.Record
}
