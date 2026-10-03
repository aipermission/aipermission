package execution

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestInitialHostTrustDurablyPublishesWithoutChangingNeighbor(t *testing.T) {
	for _, newline := range []string{"", "\n", "\n\n"} {
		t.Run("suffix-"+strings.ReplaceAll(newline, "\n", "LF"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "known_hosts")
			neighbor, key := generateHostKey(t), generateHostKey(t)
			original := knownhosts.Line([]string{"[neighbor.test]:22"}, neighbor) + newline
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			events := []string{}
			err := trustHostKeyWithOps(path, "[new.test]:22", NewUnknownHostKeyError("[new.test]:22", key).PublicKey, knownHostsFileOps{
				rename: func(from, to string) error {
					events = append(events, "rename")
					return renameKnownHostsFile(from, to)
				},
				syncDir: func(dir string) error {
					events = append(events, "sync")
					return syncKnownHostsDirectory(dir)
				},
			})
			if err != nil || strings.Join(events, ",") != "rename,sync" {
				t.Fatalf("publication events=%v, error=%v", events, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.HasPrefix(got, []byte(original)) {
				t.Fatalf("neighbor bytes changed: %q, %v", got, err)
			}
			callback, err := HostKeyCallback(path)
			if err != nil || callback("[new.test]:22", nil, key) != nil || callback("[neighbor.test]:22", nil, neighbor) != nil {
				t.Fatalf("persisted trust cannot be reopened: %v", err)
			}
			if err := TrustHostKey(path, "[new.test]:22", NewUnknownHostKeyError("[new.test]:22", key).PublicKey); err != nil {
				t.Fatalf("repeat trust: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, got) {
				t.Fatal("repeat trust appended a duplicate")
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Fatalf("staging files remain: %v, %v", entries, err)
			}
		})
	}
}

func TestInitialHostTrustDirectorySyncFailureRestoresPreviousTrust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	key := generateHostKey(t)
	failure := errors.New("directory durability failed")
	calls := 0
	err := trustHostKeyWithOps(path, "[new.test]:22", NewUnknownHostKeyError("[new.test]:22", key).PublicKey, knownHostsFileOps{
		rename: renameKnownHostsFile,
		syncDir: func(dir string) error {
			calls++
			if calls == 1 {
				return failure
			}
			return syncKnownHostsDirectory(dir)
		},
	})
	if !errors.Is(err, failure) || errors.Is(err, errKnownHostsTrustStateIndeterminate) || calls != 2 {
		t.Fatalf("sync failure/rollback = %v, calls=%d", err, calls)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || len(data) != 0 {
		t.Fatalf("failed initial trust remained accepted: %q, %v", data, readErr)
	}
	callback, err := HostKeyCallback(path)
	var unknown *UnknownHostKeyError
	if err != nil || !errors.As(callback("[new.test]:22", nil, key), &unknown) {
		t.Fatal("rolled-back trust no longer requires approval")
	}
}

func TestInitialHostTrustFailureBeforeRenamePreservesNeighbor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	neighbor, key := generateHostKey(t), generateHostKey(t)
	original := []byte(knownhosts.Line([]string{"[neighbor.test]:22"}, neighbor) + "\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("rename refused")
	err := trustHostKeyWithOps(path, "[new.test]:22", NewUnknownHostKeyError("[new.test]:22", key).PublicKey, knownHostsFileOps{
		rename:  func(string, string) error { return failure },
		syncDir: func(string) error { t.Error("sync called before successful rename"); return nil },
	})
	data, readErr := os.ReadFile(path)
	if !errors.Is(err, failure) || readErr != nil || !bytes.Equal(data, original) {
		t.Fatalf("failed publication changed original: %q, %v, %v", data, err, readErr)
	}
}
