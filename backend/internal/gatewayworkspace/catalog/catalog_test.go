package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogFileOperationsDelegateToOwnedImplementations(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.aipdb")
	target := filepath.Join(directory, "target.aipdb")
	if err := os.WriteFile(source, []byte("encrypted-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if DefaultID(target) == "" {
		t.Fatal("default database id is empty")
	}
	if err := Publish(source, target); err != nil {
		t.Fatal(err)
	}
	if err := Move(target, source); err != nil {
		t.Fatal(err)
	}
	if err := Delete(source); err != nil {
		t.Fatal(err)
	}
	if LooksPlaintext(source) {
		t.Fatal("missing file was classified as plaintext SQLite")
	}
	Scavenge(directory, time.Now())
}
