package finality

import (
	"strings"
	"testing"
)

func TestEvidencePolicyDoesNotTreatAdmissionAsDelivery(t *testing.T) {
	if InitialState(true) != "queued" || InitialState(false) != "unknown" {
		t.Fatal("initial dispatch evidence must be explicit")
	}
	where := "status = 'running' AND runtime_id = ?"
	query := RecoverySQL(where)
	if !strings.HasSuffix(query, "WHERE "+where) || strings.Count(query, "?") != 2 {
		t.Fatal("recovery lost caller scope or reason binding")
	}
	if !strings.Contains(query, "dispatch_state = 'queued' AND COALESCE(session_id, 0) = 0") ||
		!strings.Contains(query, "ELSE 'outcome_unknown'") || !strings.Contains(query, "exit_code = NULL") {
		t.Fatal("recovery asserted a remote outcome without evidence")
	}
	if !strings.Contains(ClaimSQL, "status = 'running' AND dispatch_state = 'queued'") {
		t.Fatal("dispatch admission must be one-winner and queued-only")
	}
}
