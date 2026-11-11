package mcpupload

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestMCPUploadCanceledSFTPKeepsPartialEvidenceAndPrimaryCause(t *testing.T) {
	for _, cleanupDeadline := range []bool{false, true} {
		target := directUploadTarget(t)
		root := t.TempDir()
		source := filepath.Join(root, "artifact")
		destination := filepath.Join(root, "destination")
		if err := os.WriteFile(source, make([]byte, 1<<20), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		var copied int64
		result, err := execution.UploadFileWithOptions(ctx, target, source, destination, false, execution.TransferOptions{
			Progress:      func(bytes, _ int64) { copied = bytes; cancel() },
			RecordStaging: func(context.Context, string) error { return nil },
			ClearStaging: func(context.Context, string) error {
				if cleanupDeadline {
					return context.DeadlineExceeded
				}
				return nil
			},
		})
		cancel()
		if copied <= 0 || copied >= 1<<20 || result.Bytes != copied || result.Size != 1<<20 {
			t.Fatalf("canceled SFTP lost actual evidence: result=%#v copied=%d error=%v", result, copied, err)
		}
		if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("secondary cleanup replaced primary failure: %v", err)
		}
		if cleanupDeadline && !strings.Contains(err.Error(), "clear remote staging record") {
			t.Fatalf("staging-record cleanup evidence lost: %v", err)
		}
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("canceled upload published destination: %v", err)
		}
	}
}

func directUploadTarget(t *testing.T) execution.Target {
	t.Helper()
	address, hostKey := startUploadSFTPFixture(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "disposable-upload-fixture")
	if err != nil {
		t.Fatal(err)
	}
	trust := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(trust, []byte(knownhosts.Line([]string{address.String()}, hostKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return execution.Target{Host: address.IP.String(), Port: address.Port, Username: "fixture", PrivateKey: string(pem.EncodeToMemory(block)), KnownHostsPath: trust}
}

func TestMCPUploadRejectsChangedContentWithoutReplacingApprovedDigest(t *testing.T) {
	target := directUploadTarget(t)
	root := t.TempDir()
	source := filepath.Join(root, "artifact")
	destination := filepath.Join(root, "destination")
	data := make([]byte, 1<<20)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	changed := false
	result, err := execution.UploadFileWithOptions(t.Context(), target, source, destination, false, execution.TransferOptions{
		ExpectedContent: &connectors.FileContentIdentity{SizeBytes: int64(len(data)), ChecksumSHA256: hex.EncodeToString(digest[:])},
		Progress: func(copied, _ int64) {
			if changed {
				return
			}
			changed = true
			file, err := os.OpenFile(source, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.WriteAt([]byte{1}, copied); err != nil {
				t.Fatal(err)
			}
		},
	})
	if !errors.Is(err, connectors.ErrUploadContentChanged) || result.Bytes != int64(len(data)) || result.ChecksumSHA256 != "" {
		t.Fatalf("rejected upload lost bytes or exported invalid checksum: result=%#v err=%v", result, err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("changed upload published destination: %v", err)
	}
}
