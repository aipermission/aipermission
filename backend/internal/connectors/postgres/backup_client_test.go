package postgresconnector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestMatchingPostgresDump(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		versioned    bool
		wantError    bool
	}{
		{"versioned", "pg_dump (PostgreSQL) 16.13 (Debian build)", true, false},
		{"native PATH", "pg_dump (PostgreSQL) 16.13", false, false},
		{"newer", "pg_dump (PostgreSQL) 18.0", false, true},
		{"older", "pg_dump (PostgreSQL) 15.18", false, true},
		{"malformed", "unexpected client 16", true, true},
		{"non numeric", "pg_dump (PostgreSQL) invalid", true, true},
		{"oversized", strings.Repeat("x", 4097), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := installBackupClient(t, "pg_dump", "printf '%s' "+connectors.QuoteShellArgument(tc.output))
			t.Setenv("PGPASSWORD", "must-not-reach-version-command")
			var lookedUp []string
			lookup := func(name string) (string, error) {
				lookedUp = append(lookedUp, name)
				if tc.versioned || name == "pg_dump" {
					return command, nil
				}
				return "", os.ErrNotExist
			}
			got, err := matchingPostgresDump(t.Context(), 16, lookup)
			if (err != nil) != tc.wantError || (err == nil && got != command) {
				t.Fatalf("command=%q err=%v", got, err)
			}
			if lookedUp[0] != "/usr/lib/postgresql/16/bin/pg_dump" || (!tc.versioned && len(lookedUp) != 2) {
				t.Fatalf("unexpected lookup order: %v", lookedUp)
			}
		})
	}
	if _, err := matchingPostgresDump(t.Context(), 16, func(string) (string, error) { return "", os.ErrNotExist }); err == nil || !strings.Contains(err.Error(), "postgresql-client-16") {
		t.Fatalf("missing client error=%v", err)
	}
}

func TestBackupClientProbeFailsBeforeDump(t *testing.T) {
	for _, output := range []string{"bad", "90600", "1000000", "160013\n180000", strings.Repeat("1", 4097)} {
		t.Run(output[:min(len(output), 20)], func(t *testing.T) {
			installBackupClient(t, "psql", "printf '%s' "+connectors.QuoteShellArgument(output))
			if _, _, err := postgresBackupCommand(t.Context(), postgresCLIInvocation{}); err == nil {
				t.Fatal("invalid server version accepted")
			}
		})
	}
	installBackupClient(t, "psql", "printf 'synthetic failure' >&2; exit 2")
	if _, _, err := postgresBackupCommand(t.Context(), postgresCLIInvocation{}); err == nil || !strings.Contains(err.Error(), "inspect Postgres backup server version") {
		t.Fatalf("failed probe=%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := postgresBackupCommand(ctx, postgresCLIInvocation{}); err == nil {
		t.Fatal("canceled probe succeeded")
	}
}

func TestBackupUsesMatchingClientAndRetainsConnection(t *testing.T) {
	installBackupClient(t, "psql", `test "$PGPASSWORD" = secret || exit 2
test "$PGOPTIONS" = '' || exit 3
test "$1" = --dbname || exit 4
test "$2" = postgresql://app@127.0.0.1:5432/app || exit 5
case "$*" in *'--no-psqlrc'*'SHOW server_version_num'*) printf '900001\n' ;; *) exit 6 ;; esac`)
	installBackupClient(t, "pg_dump", `if [ "$1" = --version ]; then
  test "$PGPASSWORD" = '' || exit 2
  printf 'pg_dump (PostgreSQL) 90.1\n'; exit 0
fi
test "$PGPASSWORD" = secret || exit 3
test "$1" = --dbname || exit 4
test "$2" = postgresql://app@127.0.0.1:5432/app || exit 5
case "$*" in *'--format=plain'*'--clean'*'--if-exists'*'--no-owner'*'--no-privileges'*) printf 'SELECT 1;\n' ;; *) exit 6 ;; esac`)
	t.Setenv("PGOPTIONS", "must-not-reach-client")
	artifact, err := New().Backup(t.Context(), postgresRestoreTestRuntime(), connectors.BackupRequest{})
	if err != nil || string(artifact.Data) != "SELECT 1;\n" || artifact.Metadata["server_major"] != 90 || artifact.Metadata["dump_major"] != 90 {
		t.Fatalf("artifact=%#v err=%v", artifact, err)
	}
}

func TestBackupRejectsMismatchedClientBeforeExport(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dump-started")
	installBackupClient(t, "psql", "printf '900001\n'")
	installBackupClient(t, "pg_dump", "if [ \"$1\" = --version ]; then printf 'pg_dump (PostgreSQL) 18.0'; else touch "+connectors.QuoteShellArgument(marker)+"; fi")
	if _, err := New().Backup(t.Context(), postgresRestoreTestRuntime(), connectors.BackupRequest{}); err == nil || !strings.Contains(err.Error(), "postgresql-client-90") {
		t.Fatalf("mismatched backup error=%v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dump was dispatched: %v", err)
	}
}

func TestPostgresClientOutputCancelsRunningCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "probe-started")
	command := installBackupClient(t, "psql", "touch "+connectors.QuoteShellArgument(marker)+"; exec sleep 30")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := postgresClientOutput(ctx, command, nil, os.Environ())
		finished <- err
	}()
	deadline := time.After(3 * time.Second)
	for !fileExists(marker) {
		select {
		case err := <-finished:
			t.Fatalf("probe exited before cancellation: %v", err)
		case <-deadline:
			t.Fatal("probe did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-finished:
		if err == nil || ctx.Err() == nil {
			t.Fatalf("canceled probe succeeded: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("running probe did not stop after cancellation")
	}
}

func installBackupClient(t *testing.T, name, script string) string {
	t.Helper()
	directory := t.TempDir()
	command := filepath.Join(directory, name)
	if err := os.WriteFile(command, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return command
}
