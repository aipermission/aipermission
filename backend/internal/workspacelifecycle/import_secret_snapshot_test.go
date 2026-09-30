package workspacelifecycle

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestImportSecretSnapshotIsSynchronizedWithActivation(t *testing.T) {
	content := importStagingSource(t)
	service := importStagingService(t)
	secret := "staging-gateway-secret"
	read := make(chan struct{})
	allowRead := make(chan struct{})
	releaseRead := sync.OnceFunc(func() { close(allowRead) })
	t.Cleanup(releaseRead)
	service.gatewaySecret = func() string { close(read); <-allowRead; return secret }
	service.onActivated = func(*importStagingRuntime) { secret = "activated-gateway-secret" }
	copyStarted := make(chan struct{})
	resumeCopy := make(chan struct{})
	releaseCopy := sync.OnceFunc(func() { close(resumeCopy) })
	t.Cleanup(releaseCopy)
	denied := errors.New("fixture commit denied")
	importDone := make(chan error, 1)
	go func() {
		_, err := service.Import(t.Context(), ImportInput{
			DatabaseName: "Import Copy", Password: importStagingPassword,
			Write: func(path string) error {
				close(copyStarted)
				<-resumeCopy
				return os.WriteFile(path, content, 0o600)
			},
			BeforeCommit: func() error { return denied },
		})
		importDone <- err
	}()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Fatal("secret snapshot did not begin")
	}
	activationDone := make(chan error, 1)
	go func() {
		_, err := service.Setup(t.Context(), "", "Current Workspace", importStagingPassword)
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
