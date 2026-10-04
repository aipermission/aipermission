package baselineschema_test

import (
	"slices"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db/baselineschema"
)

func TestMigrationDefinitionsAreCallerOwned(t *testing.T) {
	for name, statements := range map[string]func() []string{
		"connector": baselineschema.Connector,
		"backup":    baselineschema.BackupProviders,
		"vault":     baselineschema.ProjectVault,
	} {
		t.Run(name, func(t *testing.T) {
			original := statements()
			if len(original) == 0 {
				t.Fatal("migration definitions must not be empty")
			}
			want := slices.Clone(original)
			other := statements()
			for index := range original {
				original[index] = "caller-owned mutation"
			}
			original = append(original, "caller-owned append")
			if !slices.Equal(other, want) || !slices.Equal(statements(), want) {
				t.Fatal("caller mutation changed another migration plan")
			}
			if len(original) != len(want)+1 {
				t.Fatal("caller append was not exercised")
			}
		})
	}
}
