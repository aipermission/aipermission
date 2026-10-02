package workspacelifecycle

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestImportSecretSnapshotIsSynchronizedWithActivation(t *testing.T) {
	content := importStagingSource(t)
	service := importStagingService(t)
	// This test measures lock ownership, not encrypted runtime startup latency.
	service.open = func(_ context.Context, path, id, _ string) (*importStagingRuntime, error) {
		return &importStagingRuntime{identity: Identity{ID: id, Path: path}}, nil
	}
	service.close = func(*importStagingRuntime) error { return nil }
	ctx := t.Context()
	secret := "staging-gateway-secret"
	snapshotLocked := false
	read := make(chan struct{})
	allowRead := make(chan struct{})
	releaseRead := sync.OnceFunc(func() { close(allowRead) })
	t.Cleanup(releaseRead)
	service.gatewaySecret = func() string {
		if service.mu.TryLock() {
			service.mu.Unlock()
		} else {
			snapshotLocked = true
		}
		close(read)
		<-allowRead
		return secret
	}
	service.onActivated = func(*importStagingRuntime) { secret = "activated-gateway-secret" }
	copyStarted := make(chan struct{})
	resumeCopy := make(chan struct{})
	releaseCopy := sync.OnceFunc(func() { close(resumeCopy) })
	t.Cleanup(releaseCopy)
	denied := errors.New("fixture commit denied")
	importDone := make(chan error, 1)
	go func() {
		_, err := service.Import(ctx, ImportInput{
			DatabaseName: "Import Copy", Password: importStagingPassword,
			Write: func(path string) error {
				close(copyStarted)
				<-resumeCopy
				if err := ctx.Err(); err != nil {
					return err
				}
				if !service.mu.TryLock() {
					return errors.New("staged copy retains the service mutex after activation")
				}
				service.mu.Unlock()
				return os.WriteFile(path, content, 0o600)
			},
			BeforeCommit: func() error { return denied },
		})
		importDone <- err
	}()
	select {
	case <-read:
		if !snapshotLocked {
			t.Error("secret snapshot does not hold the service mutex")
		}
	case <-time.After(time.Second):
		t.Fatal("secret snapshot did not begin")
	}
	activationDone := make(chan error, 1)
	go func() {
		_, err := service.Setup(ctx, "", "Current Workspace", importStagingPassword)
		activationDone <- err
	}()
	select {
	case err := <-activationDone:
		releaseRead()
		releaseCopy()
		t.Fatalf("activation overlapped unsynchronized secret snapshot: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	releaseRead()
	select {
	case <-copyStarted:
	case <-time.After(time.Second):
		t.Fatal("staged copy did not begin")
	}
	select {
	case err := <-activationDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("staging retained the snapshot mutex")
	}
	releaseCopy()
	select {
	case err := <-importDone:
		if !errors.Is(err, denied) {
			t.Fatalf("staging error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("import did not finish")
	}
	if secret != "activated-gateway-secret" {
		t.Fatal("activation did not update its own secret")
	}
}
