package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

func TestRemoteRestoreStagesWithoutLifecycleAndRevalidatesCommit(t *testing.T) {
	for _, outcome := range []string{"install", "revoked", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			started, resume := make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			unblock := func() { resumeOnce.Do(func() { close(resume) }) }
			service := stalledRestoreService(t, started, resume)
			defer service.Close()
			defer unblock()
			lifecycle := newOperationOrderLifecycle(t)
			var authorized atomic.Bool
			authorized.Store(true)
			var committed, issued atomic.Bool
			candidate := filepath.Join(t.TempDir(), "candidate.aipdb")
			component := New(Dependencies{
				DataPath: filepath.Join(t.TempDir(), "workspace.aipdb"),
				Lifecycle: importCommitLifecycle{Service: lifecycle, importFn: func(_ context.Context, input workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
					if err := input.Write(candidate); err != nil {
						return workspacelifecycle.Transition{}, err
					}
					defer os.Remove(candidate)
					if err := input.BeforeCommit(); err != nil {
						return workspacelifecycle.Transition{}, err
					}
					if err := input.BeforePublish(); err != nil {
						return workspacelifecycle.Transition{}, err
					}
					committed.Store(true)
					return workspacelifecycle.Transition{Identity: workspacelifecycle.Identity{ID: "restored"}}, nil
				}},
				AcquireOperation: (&OperationLimiter{}).Acquire,
				AuthorizeImport: func(w http.ResponseWriter, _ *http.Request) (func() bool, bool) {
					return func() bool {
						if !authorized.Load() {
							http.Error(w, "session revoked", http.StatusUnauthorized)
							return false
						}
						return true
					}, true
				},
				BeginAttempt: func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) {
					return &importPasswordAttempt{}, true
				},
				IssuePrepared: func(http.ResponseWriter, uisession.Prepared) error {
					issued.Store(true)
					ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
					defer cancel()
					if release, err := lifecycle.AcquireReadContext(ctx); err == nil {
						release()
						t.Error("writer was released before session issuance")
					}
					return nil
				},
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := transientRestoreRequest(t, service.URL).WithContext(ctx)
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); component.HTTPHandlers().RestoreRemote(response, request) }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("restore never reached the download")
			}
			for _, acquire := range []func(context.Context) (func(), error){lifecycle.AcquireReadContext, lifecycle.AcquireMutationContext} {
				probe, stop := context.WithTimeout(t.Context(), 100*time.Millisecond)
				release, err := acquire(probe)
				stop()
				if err != nil {
					t.Fatalf("stalled remote download blocked lifecycle: %v", err)
				}
				release()
			}
			if outcome == "revoked" {
				authorized.Store(false)
			}
			if outcome == "canceled" {
				cancel()
			}
			unblock()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("restore did not finish")
			}
			if committed.Load() != (outcome == "install") || issued.Load() != (outcome == "install") {
				t.Fatalf("outcome=%s committed=%t issued=%t status=%d body=%s", outcome, committed.Load(), issued.Load(), response.Code, response.Body.String())
			}
			if outcome == "install" && response.Code != http.StatusOK || outcome == "revoked" && response.Code != http.StatusUnauthorized {
				t.Fatalf("restore response=%d %s", response.Code, response.Body.String())
			}
			artifacts, err := filepath.Glob(filepath.Join(filepath.Dir(component.dependencies.DataPath), ".aipermission-temp", "*"))
			if err != nil || len(artifacts) != 0 {
				t.Fatalf("restore leaked staging artifacts: %v %v", artifacts, err)
			}
			lease, err := component.acquireReadOperation(t.Context())
			if err != nil {
				t.Fatal("restore retained admission")
			}
			lease.Release()
		})
	}
}

func transientRestoreRequest(t *testing.T, baseURL string) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(TransientRestoreRequest{
		BaseURL: baseURL, Token: "fixture-token-with-thirty-two-characters", StreamID: "stream", BackupID: "version",
		DatabaseName: "Restored Copy", DatabasePassword: "FixturePassword123",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/backup/remote/restore", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestRemoteRestoreAdmissionLimitsCanceledTransfersWithoutHoldingLifecycle(t *testing.T) {
	started, resume := make(chan struct{}, 2), make(chan struct{})
	service := stalledRestoreService(t, started, resume)
	defer service.Close()
	defer close(resume)
	lifecycle := newOperationOrderLifecycle(t)
	component := New(Dependencies{
		DataPath: filepath.Join(t.TempDir(), "workspace.aipdb"), Lifecycle: lifecycle,
		AcquireOperation: (&OperationLimiter{}).Acquire,
		AuthorizeImport: func(http.ResponseWriter, *http.Request) (func() bool, bool) {
			return func() bool { return true }, true
		},
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{}, 2)
	for range 2 {
		request := transientRestoreRequest(t, service.URL).WithContext(ctx)
		go func() {
			component.HTTPHandlers().RestoreRemote(httptest.NewRecorder(), request)
			done <- struct{}{}
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("admitted restore did not reach the remote service")
		}
	}
	waitCtx, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	response := httptest.NewRecorder()
	component.HTTPHandlers().RestoreRemote(response, transientRestoreRequest(t, service.URL).WithContext(waitCtx))
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("contended restore status=%d body=%s", response.Code, response.Body.String())
	}
	probe, stopProbe := context.WithTimeout(t.Context(), time.Second)
	defer stopProbe()
	release, err := lifecycle.AcquireMutationContext(probe)
	if err != nil {
		t.Fatal("waiting restore acquired lifecycle before its operation slot")
	}
	release()
	cancel()
	for range 2 {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("canceled remote restore retained its slot")
		}
	}
	for range 2 {
		lease, err := component.acquireReadOperation(probe)
		if err != nil {
			t.Fatal("remote restore leaked operation admission")
		}
		defer lease.Release()
	}
}

func stalledRestoreService(t *testing.T, started chan<- struct{}, resume <-chan struct{}) *httptest.Server {
	t.Helper()
	payload := []byte("encrypted fixture artifact")
	digest := sha256.Sum256(payload)
	version := backups.ServiceBackup{
		ID: "version", StreamID: "stream", DatabaseName: "Original", SourceInstallationID: "fixture",
		Filename: "original.aipdb", SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]), CreatedAt: "2026-10-03T00:00:00Z",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/info":
			_ = json.NewEncoder(w).Encode(backups.ServiceInfo{
				Service: "aipermission-backup", ProtocolVersion: backups.ServiceProtocol,
				Capabilities: []string{"immutable_upload", "idempotent_upload", "upload_operation_tombstones", "list_streams", "list_versions", "download", "prune_versions", "delete_versions", "storage_usage", "automatic_retention"},
			})
		case "/v1/streams":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []backups.ServiceStream{{ID: "stream", DatabaseName: "Original"}}})
		case "/v1/streams/stream/backups":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []backups.ServiceBackup{version}})
		case "/v1/streams/stream/backups/version":
			started <- struct{}{}
			select {
			case <-r.Context().Done():
				return
			case <-resume:
			}
			w.Header().Set("X-AIPermission-Backup-ID", version.ID)
			w.Header().Set("X-AIPermission-SHA256", version.SHA256)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
}
