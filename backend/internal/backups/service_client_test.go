package backups

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const serviceTestToken = "service-client-test-token-with-thirty-two-characters"

func TestValidateServiceURL(t *testing.T) {
	valid := []string{"https://backup.example.com", "http://127.0.0.1:8080", "http://[::1]:8080"}
	for _, value := range valid {
		if _, err := ValidateServiceURL(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}
	invalid := []string{"http://localhost:8080", "http://backup.example.com", "https://backup.example.com/path", "https://user@example.com", "https://backup.example.com?q=1"}
	for _, value := range invalid {
		if _, err := ValidateServiceURL(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestServiceClientIgnoresAmbientProxyConfiguration(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example.invalid:8080")
	client, err := NewServiceClient("https://backup.example.com", serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("backup service transport must not use ambient proxy settings")
	}
}

func TestServiceClientRejectsInvalidUploadMetadata(t *testing.T) {
	client, err := NewServiceClient("http://127.0.0.1:8080", serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "input.aipdb")
	if err := os.WriteFile(input, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Upload(context.Background(), "stream-a", "bad\nname", "install-a", input); err == nil {
		t.Fatal("expected multiline database name rejection")
	}
	empty := filepath.Join(t.TempDir(), "empty.aipdb")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Upload(context.Background(), "stream-a", "Project A", "install-a", empty); err == nil {
		t.Fatal("expected empty snapshot rejection")
	}
}

func TestServiceClientLifecycleAndRedirectRejection(t *testing.T) {
	payload := []byte("encrypted-aipdb")
	digest := sha256.Sum256(payload)
	backup := ServiceBackup{
		ID: "bkp_123", StreamID: "stream-a", DatabaseName: "Project A", SourceInstallationID: "install-a",
		Filename: "project-a.aipdb", SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]), CreatedAt: "2026-07-31T12:00:00Z",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+serviceTestToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/info":
			json.NewEncoder(w).Encode(ServiceInfo{
				Service: "aipermission-backup", ProtocolVersion: ServiceProtocol, Capabilities: requiredServiceCapabilities,
			})
		case r.URL.Path == "/v1/streams":
			json.NewEncoder(w).Encode(servicePage[ServiceStream]{Items: []ServiceStream{{ID: "stream-a", DatabaseName: "Project A"}}})
		case r.URL.Path == "/v1/streams/stream-a/backups" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(backup)
		case r.URL.Path == "/v1/streams/stream-a/backups/bkp_123":
			w.Header().Set("X-AIPermission-Backup-ID", "bkp_123")
			w.Header().Set("X-AIPermission-SHA256", hex.EncodeToString(digest[:]))
			w.Header().Set("Content-Disposition", `attachment; filename="project-a.aipdb"`)
			w.Write(payload)
		case r.URL.Path == "/v1/streams/stream-a/prune" && r.Method == http.MethodPost:
			json.NewEncoder(w).Encode(ServicePruneResult{StreamID: "stream-a", KeepLatest: 2, DeletedCount: 3})
		case r.URL.Path == "/v1/streams/stream-a/backups/delete" && r.Method == http.MethodPost:
			json.NewEncoder(w).Encode(ServiceDeleteResult{StreamID: "stream-a", DeletedIDs: []string{"bkp_123"}, DeletedCount: 1})
		case r.URL.Path == "/v1/storage" && r.Method == http.MethodGet:
			remaining := int64(4096)
			json.NewEncoder(w).Encode(ServiceStorageUsage{UsedBytes: 1024, QuotaEnabled: true, QuotaBytes: 5120, RemainingBytes: &remaining, BackupCount: 1, StreamCount: 1})
		case r.URL.Path == "/v1/streams/stream-a/retention" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(ServiceRetentionPolicy{StreamID: "stream-a", Enabled: true, KeepLatest: 10})
		case r.URL.Path == "/v1/streams/stream-a/retention/preview" && r.Method == http.MethodPost:
			json.NewEncoder(w).Encode(ServiceRetentionPreview{StreamID: "stream-a", KeepLatest: 5, RetainCount: 5, RetainBytes: 900, DeleteCount: 2, DeleteBytes: 124})
		case r.URL.Path == "/v1/streams/stream-a/retention" && r.Method == http.MethodPut:
			json.NewEncoder(w).Encode(ServiceRetentionUpdate{
				Policy:       ServiceRetentionPolicy{StreamID: "stream-a", Enabled: true, KeepLatest: 5},
				Preview:      ServiceRetentionPreview{StreamID: "stream-a", KeepLatest: 5, RetainCount: 5, RetainBytes: 900, DeleteCount: 2, DeleteBytes: 124},
				DeletedCount: 2,
			})
		case r.URL.Path == "/redirect":
			http.Redirect(w, r, serverURLForRedirect(r), http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	streams, err := client.ListStreams(context.Background())
	if err != nil || len(streams) != 1 {
		t.Fatalf("list streams: items=%v err=%v", streams, err)
	}
	input := filepath.Join(t.TempDir(), "input.aipdb")
	if err := os.WriteFile(input, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := client.Upload(context.Background(), "stream-a", "Project A", "install-a", input)
	if err != nil || created.ID != "bkp_123" {
		t.Fatalf("upload: item=%#v err=%v", created, err)
	}
	pruned, err := client.PruneBackups(context.Background(), "stream-a", 2)
	if err != nil || pruned.DeletedCount != 3 {
		t.Fatalf("prune: item=%#v err=%v", pruned, err)
	}
	deleted, err := client.DeleteBackups(context.Background(), "stream-a", []string{"bkp_123"})
	if err != nil || deleted.DeletedCount != 1 {
		t.Fatalf("delete: item=%#v err=%v", deleted, err)
	}
	usage, err := client.StorageUsage(context.Background())
	if err != nil || usage.RemainingBytes == nil || *usage.RemainingBytes != 4096 {
		t.Fatalf("storage usage: item=%#v err=%v", usage, err)
	}
	policy, err := client.GetRetentionPolicy(context.Background(), "stream-a")
	if err != nil || !policy.Enabled || policy.KeepLatest != 10 {
		t.Fatalf("retention policy: item=%#v err=%v", policy, err)
	}
	preview, err := client.PreviewRetention(context.Background(), "stream-a", 5)
	if err != nil || preview.DeleteCount != 2 || preview.DeleteBytes != 124 {
		t.Fatalf("retention preview: item=%#v err=%v", preview, err)
	}
	updated, err := client.UpdateRetention(context.Background(), "stream-a", true, 5, true)
	if err != nil || updated.DeletedCount != 2 || updated.Policy.KeepLatest != 5 {
		t.Fatalf("retention update: item=%#v err=%v", updated, err)
	}
	output := filepath.Join(t.TempDir(), "output.aipdb")
	downloaded, err := client.Download(context.Background(), "stream-a", "bkp_123", output, 1024)
	if err != nil || downloaded.Filename != "project-a.aipdb" {
		t.Fatalf("download: item=%#v err=%v", downloaded, err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("downloaded bytes=%q err=%v", data, err)
	}

	request, _ := client.request(context.Background(), http.MethodGet, "/redirect", nil, false)
	if _, err := client.client.Do(request); err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}

func TestServiceClientRejectsMissingRequiredCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ServiceInfo{
			Service: "aipermission-backup", ProtocolVersion: ServiceProtocol, Capabilities: []string{"immutable_upload"},
		})
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Info(context.Background()); err == nil || !strings.Contains(err.Error(), "missing required capability") {
		t.Fatalf("expected missing capability rejection, got %v", err)
	}
}

func TestServiceClientRejectsOversizedDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Write([]byte("12345"))
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "too-large.aipdb")
	if _, err := client.Download(context.Background(), "stream-a", "bkp_123", path, 4); err == nil {
		t.Fatal("expected oversized download rejection")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("partial download was not removed: %v", err)
	}
}

func TestServiceClientRejectsUnboundedDownloadLimit(t *testing.T) {
	client, err := NewServiceClient("http://127.0.0.1:8080", serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Download(context.Background(), "stream-a", "bkp_123", filepath.Join(t.TempDir(), "backup.aipdb"), math.MaxInt64); err == nil {
		t.Fatal("expected unbounded download limit rejection")
	}
}

func TestServiceClientValidatesStreamRetentionMetadata(t *testing.T) {
	invalidKeep := -1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(servicePage[ServiceStream]{Items: []ServiceStream{{
			ID: "stream-a", DatabaseName: "Project A", BackupCount: -1, RetentionKeepLatest: &invalidKeep,
		}}})
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListStreams(context.Background()); err == nil {
		t.Fatal("expected invalid stream metadata rejection")
	}
}

func TestServiceClientValidatesRetentionUpdateEcho(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ServiceRetentionUpdate{
			Policy:       ServiceRetentionPolicy{StreamID: "stream-a", Enabled: true, KeepLatest: 6},
			Preview:      ServiceRetentionPreview{StreamID: "stream-a", KeepLatest: 6},
			DeletedCount: 1,
		})
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateRetention(context.Background(), "stream-a", true, 5, false); err == nil {
		t.Fatal("expected mismatched retention response rejection")
	}
}

func TestServiceClientRejectsMismatchedDownloadID(t *testing.T) {
	payload := []byte("encrypted-aipdb")
	digest := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-AIPermission-Backup-ID", "bkp_other")
		w.Header().Set("X-AIPermission-SHA256", hex.EncodeToString(digest[:]))
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bad-id.aipdb")
	if _, err := client.Download(context.Background(), "stream-a", "bkp_123", path, 1024); err == nil {
		t.Fatal("expected mismatched backup id rejection")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid download was not removed: %v", err)
	}
}

func TestServiceClientRejectsUploadChecksumMismatch(t *testing.T) {
	payload := []byte("encrypted-aipdb")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ServiceBackup{
			ID: "bkp_123", StreamID: "stream-a", DatabaseName: "Project A", SourceInstallationID: "install-a",
			Filename: "project-a.aipdb", SizeBytes: int64(len(payload)), SHA256: strings.Repeat("0", 64), CreatedAt: "2026-07-31T12:00:00Z",
		})
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "input.aipdb")
	if err := os.WriteFile(input, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Upload(context.Background(), "stream-a", "Project A", "install-a", input); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected upload checksum rejection, got %v", err)
	}
}

func TestServiceClientRejectsDownloadChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-AIPermission-SHA256", strings.Repeat("0", 64))
		w.Write([]byte("encrypted-aipdb"))
	}))
	defer server.Close()
	client, err := NewServiceClient(server.URL, serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bad-checksum.aipdb")
	if _, err := client.Download(context.Background(), "stream-a", "bkp_123", path, 1024); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum rejection, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid download was not removed: %v", err)
	}
}

func TestDecodeBoundedJSONRejectsOversizedAndTrailingResponses(t *testing.T) {
	var target map[string]any
	oversized := `{"value":"` + strings.Repeat("x", maxServiceJSONBytes) + `"}`
	if err := decodeBoundedJSON(strings.NewReader(oversized), &target); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected size rejection, got %v", err)
	}
	if err := decodeBoundedJSON(strings.NewReader(`{"ok":true}{"extra":true}`), &target); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func serverURLForRedirect(r *http.Request) string {
	return fmt.Sprintf("http://%s/v1/info", r.Host)
}
