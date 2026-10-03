package databasecatalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errMoveFixtureFinalSync = errors.New("fixture final move root sync failure")

type moveRollbackFixture struct {
	root, source, target string
	suffixes             []string
	markerRemoved        bool
	markerMissing        bool
	markerRemovalSynced  bool
	reverseRenames       int
}

func newMoveRollbackFixture(t *testing.T) *moveRollbackFixture {
	return newNamedMoveRollbackFixture(t, "source.db", "target.db")
}

func newNamedMoveRollbackFixture(t *testing.T, sourceName, targetName string) *moveRollbackFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := &moveRollbackFixture{
		root: root, source: filepath.Join(root, sourceName), target: filepath.Join(root, targetName),
		suffixes: []string{"", ".pre-migration-v1.aipdb", ".pre-migration-v2.aipdb.pending"},
	}
	for _, suffix := range fixture.suffixes {
		if err := os.WriteFile(fixture.source+suffix, []byte("owned artifact"+suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (f *moveRollbackFixture) operations(t *testing.T, failFinalRootSync bool) databaseMoveOps {
	t.Helper()
	ops := defaultDatabaseMoveOps()
	remove, syncDir, rename := ops.remove, ops.syncDir, ops.rename
	rootSyncs := 0
	ops.remove = func(path string) error {
		err := remove(path)
		if (err == nil || os.IsNotExist(err)) && filepath.Base(path) == databaseMoveCompleteFile {
			f.markerRemoved = true
			f.markerMissing = os.IsNotExist(err)
		}
		return err
	}
	ops.syncDir = func(path string) error {
		if path == f.root {
			rootSyncs++
			if rootSyncs == 3 && failFinalRootSync {
				return errMoveFixtureFinalSync
			}
		}
		err := syncDir(path)
		if err == nil && f.markerRemoved && strings.HasPrefix(filepath.Base(path), databaseMoveJournalPrefix) {
			f.markerRemovalSynced = true
		}
		return err
	}
	ops.rename = func(source, target string) error {
		if strings.HasPrefix(source, f.target) {
			f.reverseRenames++
			if !f.markerRemovalSynced {
				t.Errorf("reverse rename %d started before marker removal directory sync", f.reverseRenames)
			}
		}
		return rename(source, target)
	}
	return ops
}

func (f *moveRollbackFixture) assertArtifacts(t *testing.T, present, absent string) {
	t.Helper()
	for _, suffix := range f.suffixes {
		data, err := os.ReadFile(present + suffix)
		if err != nil || string(data) != "owned artifact"+suffix {
			t.Fatalf("artifact %q changed: %q %v", suffix, data, err)
		}
		if _, err := os.Lstat(absent + suffix); !os.IsNotExist(err) {
			t.Fatalf("unexpected artifact at %q: %v", absent+suffix, err)
		}
	}
}

func (f *moveRollbackFixture) journals(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(f.root, databaseMoveJournalPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestMoveRollbackSyncsMarkerRemovalBeforeRestoringArtifacts(t *testing.T) {
	fixture := newMoveRollbackFixture(t)
	err := moveDatabaseWithOps(fixture.source, fixture.target, fixture.operations(t, true))
	if !errors.Is(err, errMoveFixtureFinalSync) {
		t.Fatalf("final sync failure lost: %v", err)
	}
	if !fixture.markerRemoved || !fixture.markerRemovalSynced || fixture.reverseRenames != len(fixture.suffixes) {
		t.Fatalf("rollback order not exercised: %#v", fixture)
	}
	fixture.assertArtifacts(t, fixture.source, fixture.target)
	if paths := fixture.journals(t); len(paths) != 0 {
		t.Fatalf("successful rollback retained journal: %v", paths)
	}
}

func TestMoveRollbackMarkerSyncFailurePreservesTargetsForRecovery(t *testing.T) {
	fixture := newMoveRollbackFixture(t)
	ops := fixture.operations(t, true)
	syncDir := ops.syncDir
	markerSyncErr := errors.New("fixture removed marker directory sync failure")
	ops.syncDir = func(path string) error {
		if fixture.markerRemoved && strings.HasPrefix(filepath.Base(path), databaseMoveJournalPrefix) {
			return markerSyncErr
		}
		return syncDir(path)
	}
	err := moveDatabaseWithOps(fixture.source, fixture.target, ops)
	if !errors.Is(err, errMoveFixtureFinalSync) || !errors.Is(err, markerSyncErr) {
		t.Fatalf("rollback sync causes lost: %v", err)
	}
	if fixture.reverseRenames != 0 || fixture.markerRemovalSynced {
		t.Fatalf("failed marker sync changed artifact ownership: %#v", fixture)
	}
	fixture.assertArtifacts(t, fixture.target, fixture.source)
	assertMoveRollbackJournalRecoverable(t, fixture)
}

func TestMoveRollbackSyncsMissingMarkerBeforePartialMoveRestore(t *testing.T) {
	fixture := newMoveRollbackFixture(t)
	ops := fixture.operations(t, false)
	rename := ops.rename
	moveErr := errors.New("fixture forward snapshot move failure")
	ops.rename = func(source, target string) error {
		if source == fixture.source+fixture.suffixes[1] {
			return moveErr
		}
		return rename(source, target)
	}
	err := moveDatabaseWithOps(fixture.source, fixture.target, ops)
	if !errors.Is(err, moveErr) {
		t.Fatalf("partial move cause lost: %v", err)
	}
	if !fixture.markerMissing || !fixture.markerRemovalSynced || fixture.reverseRenames != 1 {
		t.Fatalf("missing-marker rollback boundary not exercised: %#v", fixture)
	}
	fixture.assertArtifacts(t, fixture.source, fixture.target)
	if paths := fixture.journals(t); len(paths) != 0 {
		t.Fatalf("partial move rollback retained journal: %v", paths)
	}
}

func TestMoveRollbackRecoversFailureBeforeEveryReverseRename(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("reverse_%d", failAt), func(t *testing.T) {
			fixture := newMoveRollbackFixture(t)
			ops := fixture.operations(t, true)
			rename := ops.rename
			reverseCount := 0
			reverseErr := errors.New("fixture reverse rename failure")
			ops.rename = func(source, target string) error {
				if strings.HasPrefix(source, fixture.target) {
					reverseCount++
					if !fixture.markerRemovalSynced {
						t.Error("reverse rename attempted before durable marker removal")
					}
					if reverseCount == failAt {
						return reverseErr
					}
				}
				return rename(source, target)
			}
			err := moveDatabaseWithOps(fixture.source, fixture.target, ops)
			if !errors.Is(err, errMoveFixtureFinalSync) || !errors.Is(err, reverseErr) {
				t.Fatalf("reverse rename failure lost: %v", err)
			}
			if reverseCount != len(fixture.suffixes) || !fixture.markerRemovalSynced {
				t.Fatalf("reverse boundary not exercised: count=%d synced=%v", reverseCount, fixture.markerRemovalSynced)
			}
			assertMoveRollbackJournalRecoverable(t, fixture)
		})
	}
}

func assertMoveRollbackJournalRecoverable(t *testing.T, fixture *moveRollbackFixture) {
	t.Helper()
	paths := fixture.journals(t)
	if len(paths) != 1 {
		t.Fatalf("failed rollback lost its recovery manifest: %v", paths)
	}
	if complete, err := databaseMoveJournalComplete(paths[0]); err != nil || complete {
		t.Fatalf("failed rollback remained falsely complete: %v %v", complete, err)
	}
	if info, err := os.Lstat(filepath.Join(paths[0], databaseMoveManifestFile)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("failed rollback did not retain regular manifest: %v %v", info, err)
	}
	if err := recoverDatabaseMoveJournals(fixture.root); err != nil {
		t.Fatalf("recover retained journal: %v", err)
	}
	fixture.assertArtifacts(t, fixture.source, fixture.target)
	if paths := fixture.journals(t); len(paths) != 0 {
		t.Fatalf("recovery retained journal: %v", paths)
	}
}
