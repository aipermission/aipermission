package capacityreport_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/retention/capacityreport"
)

func TestStorageWarningThresholdsAndNextRequestHeadroom(t *testing.T) {
	limits := actioncapacity.DefaultLimits()
	for _, test := range []struct {
		rows, bytes int64
		level       string
	}{
		{15999, 0, "ok"}, {16000, 0, "warning"}, {18000, 0, "critical"}, {20000, 0, "exhausted"},
		{0, limits.Bytes*80/100 + 1, "warning"}, {0, limits.Bytes*90/100 + 1, "critical"},
		{0, limits.Bytes - actioncapacity.TerminalReservationBytes, "critical"},
		{0, limits.Bytes - actioncapacity.TerminalReservationBytes + 1, "exhausted"},
	} {
		if actual := capacityreport.StorageLevel(actioncapacity.Usage{Rows: test.rows, Bytes: test.bytes}, limits); actual != test.level {
			t.Fatalf("usage=%#v level=%q", test, actual)
		}
	}
}

func TestLocalReportUsesExactTokenIDsAndExcludesSecretsAndRevokedTokens(t *testing.T) {
	database, tokenID := reportFixture(t)
	report, err := capacityreport.ReadReport(t.Context(), database, actioncapacity.DefaultLimits())
	if err != nil || len(report.Items) != 2 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	var found bool
	for _, item := range report.Items {
		if item.TokenID == strconv.FormatInt(tokenID, 10) {
			found = true
			if item.Rows != 5 || item.Running != 1 || item.Pending != 1 || item.ReservedBytes != 2*actioncapacity.TerminalReservationBytes || item.StoredBytes <= 0 {
				t.Fatalf("usage=%#v", item)
			}
		}
	}
	if !found {
		t.Fatal("lossless token identity missing")
	}
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{"opaque-fixture", "fixture-hash", "aip_capacity", "token_hash", "encrypted_payload_json"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("report leaked %q", secret)
		}
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE api_tokens SET revoked_at=datetime('now') WHERE id=?`, tokenID); err != nil {
		t.Fatal(err)
	}
	report, err = capacityreport.ReadReport(t.Context(), database, actioncapacity.DefaultLimits())
	if err != nil || len(report.Items) != 1 || report.Items[0].TokenID == strconv.FormatInt(tokenID, 10) {
		t.Fatalf("revoked token report=%#v err=%v", report, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := capacityreport.ReadReport(ctx, database, actioncapacity.DefaultLimits()); err == nil {
		t.Fatal("canceled report read succeeded")
	}
}

func reportFixture(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "report.aipdb"), "ReportFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	const tokenID = int64(9007199254740993)
	for _, id := range []int64{tokenID, tokenID + 2} {
		if _, err := database.ExecContext(t.Context(), `INSERT INTO api_tokens (id,name,token_hash,token_prefix,created_at,updated_at)
		 VALUES (?, ?, ?, 'aip_capacity', datetime('now'), datetime('now'))`, id, strconv.FormatInt(id, 10), strconv.FormatInt(id, 10)); err != nil {
			t.Fatal(err)
		}
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: "capacity_fixture", Name: "Report target"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: "capacity_fixture", Kind: "operator", Label: "Report profile"})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"completed", "failed", "outcome_unknown", "running", "approval_pending"} {
		if _, err := database.ExecContext(t.Context(), `INSERT INTO connector_action_requests
		 (token_id,target_id,profile_id,connector_kind,action_name,status,encrypted_payload_json,created_at)
		 VALUES (?, ?, ?, 'capacity_fixture','inspect',?,'opaque-fixture',datetime('now'))`, tokenID, target.ID, profile.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	return database, tokenID
}
