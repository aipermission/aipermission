package workspacelifecycle

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

const importStagingPassword = "StagedImportPassword123"

type importStagingRuntime struct {
	identity Identity
	database *sql.DB
}

func (r *importStagingRuntime) WorkspaceIdentity() Identity { return r.identity }
func (r *importStagingRuntime) WorkspaceDatabase() *sql.DB  { return r.database }

func importStagingSource(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.aipdb")
	source, err := db.OpenEncrypted(path, importStagingPassword)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	if _, err := projectvault.ResolveGatewaySecret(t.Context(), source, "staging-gateway-secret"); err != nil {
		t.Fatal(err)
	}
	if err := closeImportCandidate(source); err != nil {
		t.Fatal(err)
	}
	if db.LooksLikePlainSQLite(path) {
		t.Fatal("source fixture is not encrypted")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func importStagingService(t *testing.T) *Service[*importStagingRuntime] {
	t.Helper()
	defaultPath := filepath.Join(t.TempDir(), "default.db")
	service, err := NewService(Dependencies[*importStagingRuntime]{
		DataPath: defaultPath,
		Registry: NewRegistry(defaultPath, "default", func(r *importStagingRuntime) Identity { return r.identity }),
		Open: func(_ context.Context, path, id, password string) (*importStagingRuntime, error) {
			database, err := db.OpenEncrypted(path, password)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = database.Close() })
			return &importStagingRuntime{identity: Identity{ID: id, Path: path}, database: database}, nil
		},
		Close:         func(r *importStagingRuntime) error { return r.database.Close() },
		GatewaySecret: func() string { return "staging-gateway-secret" },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertImportStagingClean(t *testing.T, path string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			t.Errorf("candidate artifact %q remains: %v", path+suffix, err)
		}
	}
}

func TestImportStagingSlowWriterDoesNotBlockStatus(t *testing.T) {
	content := importStagingSource(t)
	service := importStagingService(t)
	started := make(chan string, 1)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := service.Import(t.Context(), ImportInput{
			DatabaseName: "Slow Copy", Password: importStagingPassword,
			Write: func(path string) error {
				started <- path
				<-release
				return os.WriteFile(path, content, 0o600)
			},
		})
		done <- err
	}()
	var candidatePath string
	select {
	case candidatePath = <-started:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("writer did not start")
	}
	statusDone := make(chan error, 1)
	go func() {
		status, err := service.Status()
		if err == nil && len(status.Databases) != 0 {
			err = errors.New("staging candidate is visible in the catalog")
		}
		statusDone <- err
	}()
	select {
	case err := <-statusDone:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("Status blocked on the import writer")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("import did not finish")
	}
	assertImportStagingClean(t, candidatePath)
	if active, ok := service.Active(); !ok || active.identity.ID != "slow-copy" {
		t.Fatalf("import was not activated: %#v", active)
	}
}

func TestImportStagingBeforeCommitRunsAfterCloseOutsideMutex(t *testing.T) {
	content := importStagingSource(t)
	service := importStagingService(t)
	var candidate *sql.DB
	var candidatePath string
	called := false
	done := make(chan error, 1)
	go func() {
		_, err := service.Import(t.Context(), ImportInput{
			DatabaseName: "Callback Copy", Password: importStagingPassword,
			Write: func(path string) error {
				candidatePath = path
				return os.WriteFile(path, content, 0o600)
			},
			Mutate: func(database *sql.DB) error {
				candidate = database
				if !service.mu.TryLock() {
					return errors.New("candidate preparation holds service mutex")
				}
				service.mu.Unlock()
				_, err := database.Exec(`CREATE TABLE staging_marker (value TEXT)`)
				return err
			},
			BeforeCommit: func() error {
				called = true
				if candidate == nil || candidate.Ping() == nil {
					return errors.New("candidate is not closed before commit callback")
				}
				if _, err := service.Status(); err != nil {
					return err
				}
				release, err := service.AcquireMutationContext(t.Context())
				if err != nil {
					return err
				}
				t.Cleanup(release)
				return nil
			},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("commit callback could not call Status outside the mutex")
	}
	if !called {
		t.Fatal("commit callback was not called")
	}
	assertImportStagingClean(t, candidatePath)
	active, ok := service.Active()
	if !ok {
		t.Fatal("import was not activated")
	}
	if _, err := active.database.Exec(`INSERT INTO staging_marker VALUES ('published')`); err != nil {
		t.Fatalf("candidate mutation was not checkpointed into the published database: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if release, err := service.AcquireReadContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("Import released the caller's lifecycle writer: %v", err)
	}
}

func TestImportStagingCancellationAndDeniedCommitCleanCandidate(t *testing.T) {
	content := importStagingSource(t)
	denied := errors.New("commit authorization denied")
	for _, stage := range []string{"before-write", "after-write", "mutate", "denied-commit", "after-callback", "before-publish"} {
		t.Run(stage, func(t *testing.T) {
			service := importStagingService(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if stage == "before-write" {
				cancel()
			}
			published, wrote, committed := false, false, false
			service.publish = func(string, string) error { published = true; return nil }
			var candidatePath string
			_, err := service.Import(ctx, ImportInput{
				DatabaseName: "Rejected Copy", Password: importStagingPassword,
				Write: func(path string) error {
					wrote, candidatePath = true, path
					if err := os.WriteFile(path, content, 0o600); err != nil {
						return err
					}
					if stage == "after-write" {
						cancel()
						for _, suffix := range []string{"-wal", "-shm", "-journal"} {
							if err := os.WriteFile(path+suffix, []byte("partial copy"), 0o600); err != nil {
								return err
							}
						}
					}
					return nil
				},
				Mutate: func(*sql.DB) error {
					if stage == "mutate" {
						cancel()
					}
					return nil
				},
				BeforeCommit: func() error {
					committed = true
					if stage == "denied-commit" {
						return denied
					}
					if stage == "after-callback" {
						cancel()
					}
					return nil
				},
				BeforePublish: func() error {
					if stage == "before-publish" {
						cancel()
					}
					return nil
				},
			})
			wantErr := error(context.Canceled)
			if stage == "denied-commit" {
				wantErr = denied
			}
			wantVerified := stage != "before-write" && stage != "after-write"
			if !errors.Is(err, wantErr) || CredentialWasVerified(err) != wantVerified || published {
				t.Fatalf("error=%v verified=%t published=%t", err, CredentialWasVerified(err), published)
			}
			if stage == "before-write" && wrote {
				t.Fatal("canceled import called Write")
			}
			if (stage == "before-write" || stage == "after-write" || stage == "mutate") && committed {
				t.Fatal("canceled staging called BeforeCommit")
			}
			if candidatePath != "" {
				assertImportStagingClean(t, candidatePath)
			}
			status, statusErr := service.Status()
			if statusErr != nil || len(status.Databases) != 0 {
				t.Fatalf("rejected import published a database: status=%#v error=%v", status, statusErr)
			}
			if _, ok := service.Active(); ok {
				t.Fatal("rejected import activated a runtime")
			}
		})
	}
}

func TestImportStagingTargetRacesPreserveExistingFile(t *testing.T) {
	content := importStagingSource(t)
	for _, stage := range []string{"before-commit", "before-publish"} {
		t.Run(stage, func(t *testing.T) {
			service := importStagingService(t)
			var candidatePath string
			targetPath, err := databasecatalog.DatabasePath(service.dataPath, "raced-copy")
			if err != nil {
				t.Fatal(err)
			}
			existing := []byte("existing target must survive")
			createTarget := func() error { return os.WriteFile(targetPath, existing, 0o600) }
			input := ImportInput{
				DatabaseName: "Raced Copy", Password: importStagingPassword,
				Write: func(path string) error {
					candidatePath = path
					return os.WriteFile(path, content, 0o600)
				},
			}
			if stage == "before-commit" {
				input.BeforeCommit = createTarget
				input.BeforePublish = func() error { t.Error("target recheck was skipped"); return nil }
			} else {
				input.BeforePublish = createTarget
			}
			_, err = service.Import(t.Context(), input)
			if !errors.Is(err, ErrDatabaseExists) || !CredentialWasVerified(err) {
				t.Fatalf("error=%v verified=%t", err, CredentialWasVerified(err))
			}
			actual, err := os.ReadFile(targetPath)
			if err != nil || !bytes.Equal(actual, existing) {
				t.Fatalf("existing target was replaced: content=%q error=%v", actual, err)
			}
			assertImportStagingClean(t, candidatePath)
			if _, ok := service.Active(); ok {
				t.Fatal("target collision activated an imported runtime")
			}
		})
	}
}

func TestImportStagingCancellationWhileWaitingForCommitMutex(t *testing.T) {
	content := importStagingSource(t)
	service := importStagingService(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prepared := make(chan string, 1)
	done := make(chan error, 1)
	published := false
	service.publish = func(string, string) error { published = true; return nil }
	go func() {
		var candidatePath string
		_, err := service.Import(ctx, ImportInput{
			DatabaseName: "Canceled Commit", Password: importStagingPassword,
			Write: func(path string) error {
				candidatePath = path
				return os.WriteFile(path, content, 0o600)
			},
			BeforeCommit: func() error {
				service.mu.Lock()
				prepared <- candidatePath
				return nil
			},
		})
		done <- err
	}()
	var candidatePath string
	select {
	case candidatePath = <-prepared:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("candidate preparation blocked on the commit mutex")
	}
	select {
	case err := <-done:
		service.mu.Unlock()
		t.Fatalf("import completed while commit mutex was held: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	service.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !CredentialWasVerified(err) || published {
			t.Fatalf("error=%v verified=%t published=%t", err, CredentialWasVerified(err), published)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled import did not finish after commit mutex was released")
	}
	assertImportStagingClean(t, candidatePath)
	status, err := service.Status()
	if err != nil || len(status.Databases) != 0 {
		t.Fatalf("canceled commit published a database: status=%#v error=%v", status, err)
	}
}
