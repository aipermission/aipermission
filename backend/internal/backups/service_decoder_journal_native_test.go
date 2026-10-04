package backups

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups/uploadoperation"
)

func TestServiceDecoderFailureKeepsSafeDurableUnknownOperation(t *testing.T) {
	database, _ := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, "http://127.0.0.1:8080", numericServiceToken, "active")
	journal := uploadoperation.NewStore(database)
	claim := uploadoperation.ClaimRequest{
		IdempotencyKey: "stable-decoder-upload", ProviderID: provider.ID, DatabaseID: "db-test",
		WorkspaceInstanceID: "instance-test", StreamID: "workspace-test", SourceInstallationID: "install-test",
	}
	if _, created, err := journal.Claim(t.Context(), claim); err != nil || !created {
		t.Fatalf("claim: created=%v err=%v", created, err)
	}
	if err := journal.MarkDispatched(t.Context(), claim.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	client, err := NewServiceClient("http://127.0.0.1:8080", numericServiceToken)
	if err != nil {
		t.Fatal(err)
	}
	transport := &decoderResponseTransport{body: io.NopCloser(strings.NewReader(`{"size_bytes":` + numericServiceToken + `}`))}
	client.client.Transport = transport
	path := filepath.Join(t.TempDir(), "snapshot.aipdb")
	if err := os.WriteFile(path, []byte("encrypted fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, replayed, err := client.Upload(t.Context(), claim.StreamID, "Test Database", claim.SourceInstallationID, claim.IdempotencyKey, path)
	if err == nil || backup != (ServiceBackup{}) || replayed {
		t.Fatalf("uncertain upload returned usable metadata: %#v replay=%v err=%v", backup, replayed, err)
	}
	markBackupUploadOutcomeUnknown(database, claim.IdempotencyKey, err)
	stored, err := journal.Get(t.Context(), claim.IdempotencyKey)
	if err != nil || stored.Status != "outcome_unknown" || stored.ProviderFileID != "" || stored.CompletedAt != nil {
		t.Fatalf("durable uncertainty changed: %#v err=%v", stored, err)
	}
	if stored.LastError != "parse backup service upload response: backup service returned invalid JSON metadata" {
		t.Fatalf("journal diagnostic differs from safe error: %q", stored.LastError)
	}
	if strings.Contains(stored.LastError, numericServiceToken) {
		t.Fatal("credential persisted in journal")
	}
	resumed, created, err := journal.Claim(t.Context(), claim)
	if err != nil || created || resumed.IdempotencyKey != stored.IdempotencyKey || resumed.Status != stored.Status {
		t.Fatalf("same-key reconciliation lost uncertainty: %#v created=%v err=%v", resumed, created, err)
	}
	if len(transport.seen) != 1 || transport.seen[0].Header.Get("X-AIPermission-Operation-ID") != claim.IdempotencyKey || transport.seen[0].Header.Get("Authorization") != "Bearer "+numericServiceToken {
		t.Fatal("upload identity changed or reconciliation dispatched another upload")
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM backup_records`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("uncertain result produced a backup record: count=%d err=%v", count, err)
	}
}
