package keycleanup

import (
	"encoding/json"
	"testing"
)

func TestAttestationPersistsHistoricalLocationCoverage(t *testing.T) {
	journal := New(newMemoryStore())
	entry := beginTest(t, journal, testIdentity(t))
	current := testIdentity(t)
	current.Host = "replacement.test"
	result, err := journal.Attest(t.Context(), entry, decisionForTest(t, entry, current, "Externally verified every historical authorization location"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Record.Attestations[0])
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		ContextDigest string `json:"deletion_context_digest"`
		Coverage      []any  `json:"coverage"`
	}
	if err := json.Unmarshal(encoded, &proof); err != nil || !validDigest(proof.ContextDigest) || len(proof.Coverage) != 2 {
		t.Fatalf("attested history has no complete location evidence: %s %v", encoded, err)
	}
}
