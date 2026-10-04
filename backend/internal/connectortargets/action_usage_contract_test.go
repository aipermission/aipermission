package connectortargets

import (
	"context"
	"database/sql"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestActionUsageMatchesNativeBytesAndSingleReservations(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, t.Context(), store)
	var completedID int64
	for _, status := range []connectors.ResultStatus{connectors.ResultCompleted, connectors.ResultRunning, connectors.ResultApprovalPending} {
		request, err := store.InsertActionRequest(t.Context(), InsertActionRequestInput{
			TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "postgres",
			ActionName: "query_readonly", Title: "Unicode \u00e9\u20ac", Reason: "byte \u00e9",
			Input: map[string]any{"value": "\u00e9\u20ac"}, Preview: map[string]any{"value": "\u20ac"}, Status: status,
			EncryptedPayloadJSON: "opaque-fixture", ApprovalContext: `{}`, ApprovalContextHash: "fixture-hash",
		})
		if err != nil {
			t.Fatal(err)
		}
		if status == connectors.ResultCompleted {
			completedID = request.ID
		}
	}
	storedBytes := nativeStoredActionBytes(t, t.Context(), database, tokenID)
	for _, incomingID := range []int64{0, completedID} {
		usage, err := actioncapacity.Measure(t.Context(), database, tokenID, incomingID)
		reservations := int64(2)
		if incomingID == completedID {
			reservations++
		}
		if err != nil || usage.Rows != 3 || usage.Running != 1 || usage.Bytes != storedBytes+reservations*(6<<20) {
			t.Fatalf("incoming=%d usage=%#v stored=%d err=%v", incomingID, usage, storedBytes, err)
		}
		limits := actioncapacity.Limits{Rows: usage.Rows, Bytes: usage.Bytes, Running: usage.Running}
		within, err := actioncapacity.Within(t.Context(), database, tokenID, incomingID, limits)
		if err != nil || !within {
			t.Fatalf("exact limits rejected: %v %v", within, err)
		}
		limits.Bytes--
		within, err = actioncapacity.Within(t.Context(), database, tokenID, incomingID, limits)
		if err != nil || within {
			t.Fatalf("one byte over accepted: %v %v", within, err)
		}
	}
}

func TestActionUsageReadsCallerTransactionAndPreservesRollback(t *testing.T) {
	database := openTargetTestDB(t)
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, t.Context(), NewStore(database))
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	request, err := NewTxStore(tx).InsertActionRequest(t.Context(), InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "postgres",
		ActionName: "query_readonly", Input: map[string]any{"value": "pending"}, Status: connectors.ResultRunning,
		EnforceTokenCapacity: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := actioncapacity.Measure(t.Context(), tx, tokenID, request.ID)
	if err != nil || usage.Rows != 1 || usage.Running != 1 || usage.Bytes < 6<<20 {
		t.Fatalf("transaction view=%#v err=%v", usage, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	usage, err = actioncapacity.Measure(t.Context(), database, tokenID, 0)
	if err != nil || usage.Rows != 0 || usage.Bytes != 0 || usage.Running != 0 {
		t.Fatalf("rolled back view=%#v err=%v", usage, err)
	}
}

func nativeStoredActionBytes(t *testing.T, ctx context.Context, database *sql.DB, tokenID int64) int64 {
	t.Helper()
	rows, err := database.QueryContext(ctx, `SELECT
		title, summary, preview_json, source, input_json, encrypted_payload_json,
		reason, status, output_json, display_text, error, approval_context,
		approval_context_hash, approval_context_drift, retry_policy_json,
		idempotency_key, idempotency_identity_hash, idempotency_scope,
		execution_owner, execution_lease_expires_at, dispatch_started_at,
		created_at, COALESCE(completed_at, '')
		FROM connector_action_requests WHERE token_id = ?`, tokenID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		values := make([]string, 23)
		destinations := make([]any, len(values))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			total += int64(len(value))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return total
}
