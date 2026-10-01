package backup

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
)

func TestDatabaseImportProxyEnvelopeMatchesBackendLimit(t *testing.T) {
	config, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "frontend", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{
		`location = /api/backup/import`,
		`location ~ ^/api/connector-targets/[0-9]+/profiles/[0-9]+/restore$`,
	} {
		pattern := regexp.MustCompile(regexp.QuoteMeta(route) + `\s*\{[^}]*client_max_body_size\s+([0-9]+)m;`)
		match := pattern.FindSubmatch(config)
		if len(match) != 2 {
			t.Fatalf("missing route-specific multipart limit for %s", route)
		}
		mib, err := strconv.ParseInt(string(match[1]), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		artifactLimit := backups.MaxDatabaseTransferBytes
		if route != `location = /api/backup/import` {
			artifactLimit = connectormanagement.MaxProfileRestoreBodyBytes
		}
		if got, want := mib<<20, artifactLimit+maxDatabaseMultipartOverhead; got != want {
			t.Fatalf("%s proxy=%d backend=%d", route, got, want)
		}
	}
}
