package runtimeactions

import "testing"

func TestRuntimeDirectoryValidationNeverTrimsPathIdentity(t *testing.T) {
	for _, path := range []string{"", " ", " directory ", "/data/ directory ", "~/ directory "} {
		actual, err := normalizeRemoteDirectoryPath(path)
		if err != nil || actual != path {
			t.Fatalf("path changed: %q -> %q (%v)", path, actual, err)
		}
	}
	for _, path := range []string{"/data\x00other", "line\nbreak", "line\rbreak"} {
		if _, err := normalizeRemoteDirectoryPath(path); err == nil {
			t.Fatal("unsafe control character accepted")
		}
	}
}
