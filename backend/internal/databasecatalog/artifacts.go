package databasecatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func databaseArtifactCandidates(base string, readDir func(string) ([]os.DirEntry, error)) ([]string, error) {
	paths := []string{base, base + "-wal", base + "-shm", base + "-journal"}
	entries, err := readDir(filepath.Dir(base))
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect database recovery artifacts: %w", err)
	}
	// Match the configured filename literally; glob metacharacters are valid names.
	prefix := filepath.Base(base) + ".pre-migration-v"
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && (strings.HasSuffix(name, ".aipdb") || strings.HasSuffix(name, ".aipdb.pending")) {
			paths = append(paths, filepath.Join(filepath.Dir(base), name))
		}
	}
	return paths, nil
}
