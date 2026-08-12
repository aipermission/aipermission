package diagnostics

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestCollectProducesGoldenRedactedReport(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "private-database-name.db"), "PrivateDatabasePassword123")
	if err != nil {
		t.Fatalf("open diagnostics fixture: %v", err)
	}
	defer database.Close()

	const forbidden = "DIAGNOSTICS_SECRET_SENTINEL"
	insertHistoryFixture(t, database, 1, "ssh", "failed", "2000-01-02 03:04:05", "dial tcp 10.20.30.40:22: connection refused "+forbidden)
	insertHistoryFixture(t, database, 2, "postgres", "outcome_unknown", time.Now().UTC().Format("2006-01-02 15:04:05"), "private path /home/operator/project "+forbidden)

	report, err := Collect(t.Context(), CollectInput{
		Database:               database,
		Registry:               connectors.NewRegistry(),
		ApplicationVersion:     "support-test",
		SupportedSchemaVersion: dbpkg.CurrentSchemaVersion(),
		MCPEnabled:             true,
		Audit: AuditHealth{
			Status: "degraded", FailureCount: 2, PendingCount: 3, RetriedEventCount: 4,
		},
		Now: func() time.Time { return time.Date(2026, 8, 11, 12, 34, 56, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("collect diagnostics: %v", err)
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	for _, value := range []string{
		forbidden, "PrivateDatabasePassword123", "private-database-name", "10.20.30.40", "/home/operator",
		"PRIVATE_TARGET_NAME", "PRIVATE_PROFILE_NAME", "PRIVATE_COMMAND", "PRIVATE_SUMMARY", "PRIVATE_INPUT", "PRIVATE_OUTPUT",
	} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("diagnostics exposed forbidden value %q: %s", value, encoded)
		}
	}

	report.Architecture = ArchitectureInfo{OS: "test", Arch: "test", GoVersion: "test"}
	report.Database.SQLCipherVersion = "test"
	for index := range report.RecentErrors {
		report.RecentErrors[index].LatestAt = "2026-08-11T00:00:00Z"
	}
	actual, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("format diagnostics: %v", err)
	}
	actual = append(actual, '\n')
	expected, err := os.ReadFile("testdata/report.golden.json")
	if err != nil {
		t.Fatalf("read diagnostics golden: %v", err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("diagnostics golden mismatch\n--- actual ---\n%s\n--- expected ---\n%s", actual, expected)
	}
}

func TestSanitizersFailClosed(t *testing.T) {
	if actual := safeIdentifier("private target/name"); actual != "unknown" {
		t.Fatalf("unsafe identifier = %q, want unknown", actual)
	}
	if actual := safeVersion("0.2.25/private-path"); actual != "unknown" {
		t.Fatalf("unsafe version = %q, want unknown", actual)
	}
	if actual := safeTimestamp("private timestamp"); actual != "" {
		t.Fatalf("unsafe timestamp = %q, want empty", actual)
	}
}

func TestClassifyErrorUsesBoundedCategories(t *testing.T) {
	tests := []struct{ status, message, expected string }{
		{"failed", "context deadline exceeded", "timeout"},
		{"failed", "x509: certificate is expired", "tls"},
		{"failed", "password authentication failed", "authentication"},
		{"failed", "permission denied", "authorization"},
		{"failed", "dial tcp: connection refused", "network"},
		{"failed", "object does not exist", "not_found"},
		{"failed", "resource already exists", "conflict"},
		{"failed", "invalid request", "validation"},
		{"failed", "internal server error", "internal"},
		{"failed", "opaque secret detail", "other"},
		{"blocked", "opaque secret detail", "permission"},
		{"stale", "opaque secret detail", "context_drift"},
		{"outcome_unknown", "opaque secret detail", "outcome_unknown"},
	}
	for _, test := range tests {
		if actual := classifyError(test.status, test.message); actual != test.expected {
			t.Errorf("classifyError(%q, %q) = %q, want %q", test.status, test.message, actual, test.expected)
		}
	}
}

func insertHistoryFixture(t *testing.T, database *sql.DB, id int, connectorKind, status, timestamp, errorText string) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, target_name, profile_label,
		status, title, summary, input_text, input_json, output_text, output_json, error, created_at, updated_at
	) VALUES ('fixture', ?, ?, 'action', 'PRIVATE_TARGET_NAME', 'PRIVATE_PROFILE_NAME', ?,
		'PRIVATE_COMMAND', 'PRIVATE_SUMMARY', 'PRIVATE_INPUT', '{"secret":"PRIVATE_INPUT"}',
		'PRIVATE_OUTPUT', '{"secret":"PRIVATE_OUTPUT"}', ?, ?, ?)`,
		id, connectorKind, status, errorText, timestamp, timestamp,
	)
	if err != nil {
		t.Fatalf("insert diagnostics history fixture: %v", err)
	}
}
