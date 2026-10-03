package execution

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failedKnownHostsCandidate struct {
	*os.File
	stage   string
	failure error
	closes  int
}

func (file *failedKnownHostsCandidate) Chmod(mode os.FileMode) error {
	if file.stage == "chmod" {
		return file.failure
	}
	return file.File.Chmod(mode)
}

func (file *failedKnownHostsCandidate) Write(data []byte) (int, error) {
	if file.stage == "write" {
		n, err := file.File.Write(data[:min(3, len(data))])
		return n, errors.Join(file.failure, err)
	}
	return file.File.Write(data)
}

func (file *failedKnownHostsCandidate) Sync() error {
	if file.stage == "sync" {
		return file.failure
	}
	return file.File.Sync()
}

func (file *failedKnownHostsCandidate) Close() error {
	file.closes++
	err := file.File.Close()
	if file.stage == "close" {
		return errors.Join(file.failure, err)
	}
	return err
}

func TestKnownHostsCandidateFailureCleansOwnedFile(t *testing.T) {
	for _, pattern := range []string{".known_hosts.rollback-*", ".known_hosts.tmp-*"} {
		for _, stage := range []string{"chmod", "write", "sync", "close"} {
			t.Run(pattern+"/"+stage, func(t *testing.T) {
				dir := t.TempDir()
				original := []byte("original trust bytes\n")
				destination := filepath.Join(dir, "known_hosts")
				if err := os.WriteFile(destination, original, 0o600); err != nil {
					t.Fatal(err)
				}
				created, err := os.CreateTemp(dir, pattern)
				if err != nil {
					t.Fatal(err)
				}
				failure := errors.New("owned staging I/O failed")
				file := &failedKnownHostsCandidate{File: created, stage: stage, failure: failure}
				path, err := finishKnownHostsCandidate(file, []byte("candidate bytes\n"), 0o600)
				if !errors.Is(err, failure) || file.closes != 1 || (stage != "close" && path != "") {
					t.Fatalf("staging result=%q err=%v closes=%d", path, err, file.closes)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 1 || entries[0].Name() != "known_hosts" {
					t.Fatalf("owned staging artifact survived: %v %v", entries, err)
				}
				got, err := os.ReadFile(destination)
				if err != nil || !bytes.Equal(got, original) {
					t.Fatal("original trust changed")
				}
			})
		}
	}
}
