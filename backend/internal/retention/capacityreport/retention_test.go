package capacityreport_test

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
	"github.com/aipermission/aipermission/backend/internal/retention/sqlstore"
)

func TestRetentionReleasesCapacityAtomicallyWithoutLosingReplayEvidence(t *testing.T) {
	database, tokenID := reportFixture(t)
	ctx := t.Context()
	if _, err := database.ExecContext(ctx, `UPDATE connector_action_requests
	 SET completed_at=datetime('now','-10 days'), idempotency_key='retained-capacity-result',
	 idempotency_identity_hash='capacity-identity', idempotency_scope='mcp'
	 WHERE token_id=? AND status='completed'`, tokenID); err != nil {
		t.Fatal(err)
	}
	before, err := actioncapacity.Measure(ctx, database, tokenID, 0)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := (sqlstore.Store{}).PurgeHistory(ctx, tx, "-7 days"); err != nil {
		t.Fatal(err)
	}
	within, err := actioncapacity.Measure(ctx, tx, tokenID, 0)
	if err != nil || within.Rows != before.Rows-1 || within.Bytes >= before.Bytes || within.Running != before.Running {
		t.Fatalf("purge usage=%#v before=%#v err=%v", within, before, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	after, err := actioncapacity.Measure(ctx, database, tokenID, 0)
	if err != nil || after != before {
		t.Fatalf("rollback usage=%#v before=%#v err=%v", after, before, err)
	}
	if _, err := (sqlstore.Store{}).PurgeHistory(ctx, database, "-7 days"); err != nil {
		t.Fatal(err)
	}
	after, err = actioncapacity.Measure(ctx, database, tokenID, 0)
	if err != nil || after != within {
		t.Fatalf("committed usage=%#v expected=%#v err=%v", after, within, err)
	}
	var tombstones int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM connector_action_idempotency_tombstones
	 WHERE token_id=? AND idempotency_key='retained-capacity-result'`, tokenID).Scan(&tombstones); err != nil || tombstones != 1 {
		t.Fatalf("replay tombstones=%d err=%v", tombstones, err)
	}
}
