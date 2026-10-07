package conformance_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func assertRestoreRestrictedArtifact(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	restorer := connector.(connectors.BackupRestorer)
	canary := filepath.Join(t.TempDir(), "must-not-exist")
	malicious := "COPY (SELECT 1) TO STDOUT; -- FROM STDIN;\n\\! touch " + connectors.QuoteShellArgument(canary) + "\n\\.\n"
	_, err := restorer.Restore(t.Context(), runtime, connectors.RestoreRequest{
		Filename: "malicious.sql", Content: strings.NewReader(malicious), Size: int64(len(malicious)),
	})
	if err == nil {
		t.Fatal("comment-masked shell command was accepted")
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("restore shell canary executed: %v", err)
	}
	lexicalMismatch := "SET standard_conforming_strings=off;\nSELECT '\\'';\n\\! touch " + connectors.QuoteShellArgument(canary) + "\n-- '\n"
	_, err = restorer.Restore(t.Context(), runtime, connectors.RestoreRequest{
		Filename: "lexical-mismatch.sql", Content: strings.NewReader(lexicalMismatch), Size: int64(len(lexicalMismatch)),
	})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("post-dispatch restricted-mode rejection = %v", err)
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("post-dispatch shell canary executed: %v", err)
	}
	script := "\\restrict originaldumpkey\n" +
		"CREATE TABLE public.aipermission_restricted_copy (value text);\n" +
		"COPY public.aipermission_restricted_copy (value)\nFROM /* source */ STDIN;\n\\N\n\\unrestrict literal-data\n\\.\n" +
		"-- after copy\nDROP TABLE public.aipermission_restricted_copy;\n\\unrestrict originaldumpkey\n"
	result, err := restorer.Restore(t.Context(), runtime, connectors.RestoreRequest{
		Filename: "copy.sql", Content: strings.NewReader(script), Size: int64(len(script)),
	})
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("real restricted COPY restore result=%#v err=%v", result, err)
	}
	assertRestoreLiteralCopyMarkers(t, restorer, runtime)
	artifact, err := restorer.Backup(t.Context(), runtime, connectors.BackupRequest{})
	if err != nil {
		t.Fatalf("real pg_dump: %v", err)
	}
	result, err = restorer.Restore(t.Context(), runtime, connectors.RestoreRequest{
		Filename: artifact.Filename, Content: bytes.NewReader(artifact.Data), Size: int64(len(artifact.Data)),
	})
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("real pg_dump roundtrip result=%#v err=%v", result, err)
	}
}

func assertRestoreLiteralCopyMarkers(t *testing.T, restorer connectors.BackupRestorer, runtime connectors.RuntimeContext) {
	t.Helper()
	script := "CREATE TABLE public.aipermission_literal_copy (value text);\nCOPY public.aipermission_literal_copy FROM STDIN WITH (FORMAT csv);\n \\.\n\\restrict k\n\\unrestrict k\n/*\n\\.\n--*/\nSELECT 1;\n"
	result, err := restorer.Restore(t.Context(), runtime, connectors.RestoreRequest{
		Content: strings.NewReader(script), Size: int64(len(script)),
	})
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("literal COPY result=%#v err=%v", result, err)
	}
	conn := connectPostgresPolicyFixture(t)
	defer conn.Close(context.Background())
	defer conn.Exec(context.Background(), `DROP TABLE public.aipermission_literal_copy`)
	var values []string
	if err := conn.QueryRow(t.Context(), `SELECT array_agg(value ORDER BY ctid) FROM public.aipermission_literal_copy`).Scan(&values); err != nil {
		t.Fatal(err)
	}
	if strings.Join(values, "\n") != " \\.\n\\restrict k\n\\unrestrict k\n/*" {
		t.Fatalf("COPY data changed: %q", values)
	}
}
