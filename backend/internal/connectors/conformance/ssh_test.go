package conformance_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestOpenSSHRealService(t *testing.T) {
	requireProtocolFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	target := protocolSSHTarget(t)
	client, err := execution.DialSSH(ctx, target)
	if client != nil {
		_ = client.Close()
	}
	var unknown *execution.UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("unapproved OpenSSH identity was not refused: %v", err)
	}
	assertOwnedSSHIdentity(t, unknown)
	if err := execution.TrustHostKey(target.KnownHostsPath, "protocols:22", unknown.PublicKey); err != nil {
		t.Fatal(err)
	}
	result, err := execution.RunCommand(ctx, target, "printf 'stdout'; printf 'stderr' >&2; exit 7")
	if err != nil || result.Stdout != "stdout" || result.Stderr != "stderr" || result.ExitCode != 7 || !result.DispatchStarted {
		t.Fatalf("OpenSSH execution contract: %#v / %v", result, err)
	}
	assertSSHChangedIdentityRejected(t, ctx, target)
	assertSSHCancellation(t, ctx, target)
	assertSFTPAtomicRoundTrip(t, ctx, target)
}

func assertOwnedSSHIdentity(t *testing.T, unknown *execution.UnknownHostKeyError) {
	t.Helper()
	encoded, err := os.ReadFile("/fixture-material/ssh_host.pub")
	if err != nil {
		t.Fatal(err)
	}
	expected, _, _, _, err := ssh.ParseAuthorizedKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := execution.ParseHostPublicKey(unknown.PublicKey)
	if err != nil || !bytes.Equal(expected.Marshal(), observed.Marshal()) {
		t.Fatal("SSH approval did not identify the owned daemon key")
	}
}

func assertSSHChangedIdentityRejected(t *testing.T, ctx context.Context, target execution.Target) {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	wrong := execution.NewUnknownHostKeyError("protocols:22", key)
	if err := execution.ReplaceHostKey(target.KnownHostsPath, "protocols:22", wrong.PublicKey); err != nil {
		t.Fatal(err)
	}
	result, err := execution.RunCommand(ctx, target, "printf should-not-execute")
	var changed *execution.ChangedHostKeyError
	if !errors.As(err, &changed) || result.DispatchStarted || result.Stdout != "" {
		t.Fatalf("changed OpenSSH identity was not refused before dispatch: %#v / %v", result, err)
	}
	if err := execution.ReplaceHostKey(target.KnownHostsPath, "protocols:22", changed.PublicKey); err != nil {
		t.Fatal(err)
	}
}

func assertSSHCancellation(t *testing.T, ctx context.Context, target execution.Target) {
	t.Helper()
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	started := time.Now()
	result, err := execution.StreamCommand(canceled, target, "printf ready; exec sleep 20", func(_ []byte) { cancel() }, nil)
	if !errors.Is(err, context.Canceled) || !result.DispatchStarted || time.Since(started) > 5*time.Second {
		t.Fatalf("OpenSSH cancellation did not stop a dispatched command promptly: %#v / %v", result, err)
	}
}

func assertSFTPAtomicRoundTrip(t *testing.T, ctx context.Context, target execution.Target) {
	t.Helper()
	remote := fmt.Sprintf("/home/aipermission/conformance-%x/payload.bin", time.Now().UnixNano())
	directory := filepath.Dir(remote)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := execution.RunCommand(cleanup, target, "rm -rf -- "+directory)
		if err != nil || result.ExitCode != 0 {
			t.Errorf("remove owned SSH transfer fixture: %v / %d", err, result.ExitCode)
		}
	})
	local := filepath.Join(t.TempDir(), "payload.bin")
	payload := []byte{0, 255, 10, 13, 39, 92, 195, 169}
	if err := os.WriteFile(local, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.UploadFile(ctx, target, local, remote, false, nil); err != nil {
		t.Fatal(err)
	}
	client, err := execution.DialSSH(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	files, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.Chmod(remote, 0640); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), payload...)
	payload = append(payload, 17)
	if err := os.WriteFile(local, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.UploadFile(ctx, target, local, remote, false, nil); err == nil {
		t.Fatal("OpenSSH upload silently overwrote an existing file")
	}
	assertSFTPContent(t, files, remote, original)
	assertSFTPStagingCommit(t, ctx, target, files, local, remote, original, int64(len(payload)))
	stat, err := files.Stat(remote)
	if err != nil || stat.Mode().Perm() != 0640 || stat.Size() != int64(len(payload)) {
		t.Fatalf("SFTP atomic overwrite lost metadata: %#v / %v", stat, err)
	}
	down := filepath.Join(t.TempDir(), "download.bin")
	if _, err := execution.DownloadFile(ctx, target, remote, down, nil); err != nil {
		t.Fatal(err)
	}
	readback, err := os.ReadFile(down)
	if err != nil || !bytes.Equal(payload, readback) {
		t.Fatalf("SFTP binary roundtrip changed content: %x / %v", readback, err)
	}
	entries, err := execution.ListRemoteDirectory(ctx, target, directory)
	if err != nil || len(entries) != 1 || entries[0].Name != "payload.bin" {
		t.Fatalf("SFTP staging was not cleaned: %#v / %v", entries, err)
	}
}
