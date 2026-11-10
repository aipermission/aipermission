package actions

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestUploadBudgetChangesInvalidateApproval(t *testing.T) {
	prepared := PreparedRequest{ActionDefinition: connectors.ActionDefinition{
		Name: "upload", Risk: connectors.RiskWrite, MaxUploadBytes: connectors.MaximumFileUploadBytes,
	}}
	previous, previousHash, err := BuildApprovalContext(prepared, ApprovalTokenSnapshot{ID: 1}, ApprovalPermissionSnapshot{Rule: "approval_required"}, "first")
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []int64{1, 0} {
		changed := prepared
		changed.ActionDefinition.MaxUploadBytes = next
		current, currentHash, err := BuildApprovalContext(changed, ApprovalTokenSnapshot{ID: 1}, ApprovalPermissionSnapshot{Rule: "approval_required"}, "second")
		if err != nil {
			t.Fatal(err)
		}
		if previousHash == currentHash || ApprovalDriftReason(previous, current) != "action_definition" {
			t.Fatalf("upload budget drift ignored: next=%d", next)
		}
	}
}
