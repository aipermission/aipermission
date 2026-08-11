package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyAuditMutationHelperDoesNotReturn(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), "writeAudit"+"(") {
			t.Errorf("%s uses the ambiguous legacy audit helper; use the transactional mutation boundary or writeObservationAudit", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
