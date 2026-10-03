package execution

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostTrustOperationsValidateBeforeCreatingFiles(t *testing.T) {
	for name, operation := range map[string]func(string, string, string) error{"trust": TrustHostKey, "replace": ReplaceHostKey} {
		for _, scenario := range []struct{ name, path, hostname, message string }{
			{"empty path", "", "", "known_hosts path is required"},
			{"dot path", ".", "", "known_hosts path is required"},
			{"empty hostname", "nested/known_hosts", " \t", "hostname is required"},
			{"invalid key", "nested/known_hosts", "[host.test]:22", "decode host public key:"},
		} {
			t.Run(name+"/"+scenario.name, func(t *testing.T) {
				dir := t.TempDir()
				path := scenario.path
				if path != "" && path != "." {
					path = filepath.Join(dir, path)
				}
				err := operation(path, scenario.hostname, "bad!")
				if err == nil || !strings.HasPrefix(err.Error(), scenario.message) {
					t.Fatalf("validation=%v", err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("invalid input created files: %v %v", entries, err)
				}
			})
		}
	}
}

func TestNormalizedHostTrustInputRetainsDistinctAcceptancePolicies(t *testing.T) {
	dir := t.TempDir()
	path := dir + string(filepath.Separator) + "unused" + string(filepath.Separator) + ".." + string(filepath.Separator) + "known_hosts"
	hostname := "[host.test]:22"
	first, replacement := generateHostKey(t), generateHostKey(t)
	firstPublic := NewUnknownHostKeyError(hostname, first).PublicKey
	replacementPublic := NewUnknownHostKeyError(hostname, replacement).PublicKey
	if err := TrustHostKey(path, " "+hostname+" ", " "+firstPublic+" "); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceHostKey(path, " "+hostname+" ", replacementPublic); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := TrustHostKey(path, hostname, firstPublic); err == nil {
		t.Fatal("initial trust silently replaced a changed key")
	}
	after, err := os.ReadFile(filepath.Clean(path))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("denied trust changed replacement bytes")
	}
	callback, err := HostKeyCallback(path)
	if err != nil || callback(hostname, nil, replacement) != nil {
		t.Fatalf("replacement trust lost: %v", err)
	}
}
