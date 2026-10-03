package databasecatalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredDatabaseFilenameMoveRecovery(t *testing.T) {
	for _, sourceName := range []string{"source.db", "source.aipdb", "source", "source.sqlite", "custom.[db]"} {
		for _, failAt := range []int{1, 2, 3, 4, 5, 6} {
			t.Run(fmt.Sprintf("%s/reverse_%d", sourceName, failAt), func(t *testing.T) {
				fixture := newNamedMoveRollbackFixture(t, sourceName, "renamed.db")
				for _, suffix := range []string{"-wal", "-shm", "-journal"} {
					fixture.suffixes = append(fixture.suffixes, suffix)
					if err := os.WriteFile(fixture.source+suffix, []byte("owned artifact"+suffix), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				ops := fixture.operations(t, true)
				rename := ops.rename
				count := 0
				reverseErr := errors.New("fixture interrupted rollback")
				ops.rename = func(source, target string) error {
					if strings.HasPrefix(source, fixture.target) {
						count++
						if count == failAt {
							return reverseErr
						}
					}
					return rename(source, target)
				}
				err := moveDatabaseWithOps(fixture.source, fixture.target, ops)
				if !errors.Is(err, errMoveFixtureFinalSync) || !errors.Is(err, reverseErr) || count != len(fixture.suffixes) {
					t.Fatalf("rollback boundary not exercised: %v count=%d", err, count)
				}
				assertMoveRollbackJournalRecoverable(t, fixture)
				if _, err := ListDatabases(fixture.source, fixture.source); err != nil {
					t.Fatalf("recovered default cannot be listed: %v", err)
				}
			})
		}
	}
}

func TestMoveRejectsUnrecoverableManifestBeforeMutation(t *testing.T) {
	fixture := newMoveRollbackFixture(t)
	for index := 3; index < 65; index++ {
		suffix := fmt.Sprintf(".pre-migration-v%d.aipdb", index)
		fixture.suffixes = append(fixture.suffixes, suffix)
		if err := os.WriteFile(fixture.source+suffix, []byte("owned artifact"+suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mutated := false
	ops := defaultDatabaseMoveOps()
	ops.mkdir = func(string, os.FileMode) error { mutated = true; return errors.New("unexpected journal creation") }
	ops.rename = func(string, string) error { mutated = true; return errors.New("unexpected rename") }
	if err := moveDatabaseWithOps(fixture.source, fixture.target, ops); err == nil || !strings.Contains(err.Error(), "recovery root") {
		t.Fatalf("oversized manifest was not rejected: %v", err)
	}
	if mutated || len(fixture.journals(t)) != 0 {
		t.Fatal("invalid manifest mutated the catalog")
	}
	fixture.assertArtifacts(t, fixture.source, fixture.target)
}

func TestMoveManifestFilenameDoesNotRelaxPathBounds(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"source.aipdb", "source", "source.sqlite"} {
		source, target := filepath.Join(root, name), filepath.Join(root, "renamed.db")
		for _, path := range []string{filepath.Join(filepath.Dir(root), name), filepath.Join(root, "nested", name)} {
			manifest := databaseMoveManifest{SourceBase: path, TargetBase: target, Moves: []databaseMove{{Source: path, Target: target}}}
			if err := validateDatabaseMoveManifest(root, manifest); err == nil {
				t.Fatalf("unsafe base accepted: %q", path)
			}
		}
		manifest := databaseMoveManifest{SourceBase: source, TargetBase: target, Moves: []databaseMove{{Source: source + ".unrelated", Target: target + ".unrelated"}}}
		if err := validateDatabaseMoveManifest(root, manifest); err == nil {
			t.Fatal("unowned suffix accepted")
		}
	}
}

func TestMovePublicationUsesRecoveryPathPolicy(t *testing.T) {
	fixture := newMoveRollbackFixture(t)
	target := filepath.Join(fixture.root, "nested", "target.db")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := moveDatabaseWithOps(fixture.source, target, defaultDatabaseMoveOps()); err == nil || !strings.Contains(err.Error(), "catalog directory") {
		t.Fatalf("producer accepted a recovery-invalid directory: %v", err)
	}
	fixture.assertArtifacts(t, fixture.source, target)
	if len(fixture.journals(t)) != 0 {
		t.Fatal("invalid producer published a journal")
	}
}

func TestLiteralDatabaseFilenameArtifactOwnership(t *testing.T) {
	for _, operation := range []string{"move", "delete"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newNamedMoveRollbackFixture(t, "custom.[db]", "target.db")
			neighbor := filepath.Join(fixture.root, "custom.d.pre-migration-v1.aipdb")
			if err := os.WriteFile(neighbor, []byte("unrelated snapshot"), 0o600); err != nil {
				t.Fatal(err)
			}
			if operation == "move" {
				if err := moveDatabaseWithOps(fixture.source, fixture.target, defaultDatabaseMoveOps()); err != nil {
					t.Fatal(err)
				}
				fixture.assertArtifacts(t, fixture.target, fixture.source)
			} else {
				if err := deleteDatabaseWithOps(fixture.source, defaultDatabaseDeleteOps()); err != nil {
					t.Fatal(err)
				}
				for _, suffix := range fixture.suffixes {
					if _, err := os.Lstat(fixture.source + suffix); !os.IsNotExist(err) {
						t.Fatalf("owned artifact was retained: %q %v", suffix, err)
					}
				}
			}
			if data, err := os.ReadFile(neighbor); err != nil || string(data) != "unrelated snapshot" {
				t.Fatalf("neighbor changed: %q %v", data, err)
			}
		})
	}
}
