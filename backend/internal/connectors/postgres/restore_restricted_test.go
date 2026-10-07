package postgresconnector

import (
	"io"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestRestoreStagesGatewayOwnedRestriction(t *testing.T) {
	content := "\\restrict dumpkey\nCREATE TABLE public.items (value text);\nCOPY public.items (value)\nFROM /* source */ STDIN WITH (FORMAT csv);\n \\.\n\\restrict copydata\n\\unrestrict copydata\n\\.\n-- after COPY\n\\unrestrict dumpkey\nSELECT 1;\n"
	reader, cleanup, _, err := validatedPostgresRestoreContent(t.Context(), connectors.RestoreRequest{
		Content: strings.NewReader(content), Size: int64(len(content)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	staged, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(staged), "dumpkey") || !strings.Contains(string(staged), " \\.\n\\restrict copydata\n\\unrestrict copydata\n\\.\n") {
		t.Fatalf("dump markers or COPY data incorrectly staged: %q", staged)
	}
}

func TestRestoreUsesUnpredictableRestrictionAroundValidatedStream(t *testing.T) {
	directory := t.TempDir()
	installFakePSQL(t, directory, `
IFS= read -r first
case "$first" in "\restrict "*) key="${first#* }" ;; *) exit 2 ;; esac
case "$key" in *[!a-f0-9]*|"") exit 3 ;; esac
restricted=true
while IFS= read -r line; do
  case "$line" in
    "\restrict "*) exit 4 ;;
    "\unrestrict "*) test "${line#* }" = "$key" || exit 5; restricted=false ;;
    "\echo "*) test "$restricted" = false || exit 6; printf '%s\n' "${line#* }" ;;
  esac
done`)
	content := "\\restrict dumpkey\nSELECT 1;\n\\unrestrict dumpkey\n"
	result, err := New().Restore(t.Context(), postgresRestoreTestRuntime(), connectors.RestoreRequest{
		Content: strings.NewReader(content), Size: int64(len(content)),
	})
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("restricted restore result=%#v err=%v", result, err)
	}
}
