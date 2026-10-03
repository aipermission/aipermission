package snapshotfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyExclusiveArtifactOwnership(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("encrypted artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Copy(source)(target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("candidate mode: %v %v", info, err)
	}
	if err := Copy(source)(target); !os.IsExist(err) {
		t.Fatalf("existing candidate was overwritten: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "encrypted artifact" {
		t.Fatalf("existing candidate changed: %q %v", data, err)
	}
	if err := Copy(filepath.Join(root, "missing"))(target); !os.IsNotExist(err) {
		t.Fatalf("missing source: %v", err)
	}
	failed := filepath.Join(root, "failed")
	if err := Copy(root)(failed); err == nil {
		t.Fatal("directory source was accepted")
	}
	if _, err := os.Stat(failed); !os.IsNotExist(err) {
		t.Fatalf("failed copy retained owned candidate: %v", err)
	}
}

func TestStageRemovesOnlyOwnedFailures(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign", true: "owned"}[owned], func(t *testing.T) {
			var reserved string
			want := errors.New("write refused")
			path, err := Stage(filepath.Join(t.TempDir(), "workspace.aipdb"), "candidate-*", func(path string) (bool, error) {
				reserved = path
				if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
				return owned, want
			})
			if !errors.Is(err, want) || path != "" {
				t.Fatalf("staging failure = %q %v", path, err)
			}
			_, statErr := os.Stat(reserved)
			if owned != os.IsNotExist(statErr) {
				t.Fatalf("cleanup ownership=%t: %v", owned, statErr)
			}
		})
	}
	root := t.TempDir()
	path, err := Stage(filepath.Join(root, "workspace.aipdb"), "candidate-*", func(path string) (bool, error) {
		return true, os.WriteFile(path, []byte("verified"), 0o600)
	})
	if err != nil || path == "" {
		t.Fatalf("successful staging: %q %v", path, err)
	}
	if _, err := Stage(filepath.Join(path, "workspace.aipdb"), "candidate-*", func(string) (bool, error) {
		t.Fatal("writer called after reservation failure")
		return false, nil
	}); err == nil {
		t.Fatal("invalid staging root accepted")
	}
}
