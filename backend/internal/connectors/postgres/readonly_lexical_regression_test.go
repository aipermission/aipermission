package postgresconnector

import (
	"context"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestReadonlyPolicyRejectsHiddenLexicalCalls(t *testing.T) {
	for _, query := range []string{
		"SELECT pg_catalog.pg_read_file\v(E'/etc/passwd')",
		"SELECT public.dblink_exec\v('dbname=app', 'delete from users')",
		"SELECT lo_export\v(1, '/tmp/fixture')",
		"SELECT 1 OPERATOR\v(pg_catalog.+) 1",
		"SELECT CAST\v('1' AS int)",
		"SELECT public.mytype\v'x'",
		"SELECT 1--\r, pg_catalog.pg_read_file(E'/etc/passwd')",
		"SELECT 1--\r; INSERT INTO fixture VALUES (1)",
	} {
		t.Run(query, func(t *testing.T) {
			request := connectors.ActionRequest{ActionName: ActionQueryReadonly,
				Target:  connectors.TargetView{ID: 1, Ref: "postgres:1:1", ConnectorKind: Kind},
				Profile: connectors.CredentialProfileView{ID: 1, TargetID: 1, ConnectorKind: Kind, Kind: "username_password"},
				Input:   map[string]any{"sql": query},
			}
			if _, err := New().PrepareAction(context.Background(), request); err == nil {
				t.Fatal("unsafe SQL accepted during preparation")
			}
			request.Input["sql"] = "SELECT 1"
			prepared, err := New().PrepareAction(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			prepared.Payload["sql"] = query
			runtime := connectors.RuntimeContext{Target: request.Target, Profile: request.Profile}
			_, err = New().ExecuteAction(context.Background(), runtime, prepared)
			policyError := validateReadonlySQL(query)
			if err == nil || policyError == nil || err.Error() != policyError.Error() {
				t.Fatalf("persisted SQL was not rejected before transport: %v", err)
			}
		})
	}
}

func TestReadonlyPolicyRecognizesAllPostgresWhitespace(t *testing.T) {
	for _, whitespace := range []string{" ", "\t", "\n", "\r", "\r\n", "\f", "\v"} {
		for _, query := range []string{
			"SELECT" + whitespace + "lower" + whitespace + "('A')",
			"SELECT 1--" + whitespace + "\n, lower('A')",
		} {
			if err := validateReadonlySQL(query); err != nil {
				t.Fatalf("valid SQL %q: %v", query, err)
			}
		}
		if err := validateReadonlySQL("SELECT pg_catalog.pg_read_file" + whitespace + "('fixture')"); err == nil {
			t.Fatalf("unapproved function hidden by %q", whitespace)
		}
	}
}

func TestReadonlyPolicyRejectsUnsupportedUnquotedControls(t *testing.T) {
	for character := byte(1); character < 32; character++ {
		if strings.ContainsRune("\t\n\r\f\v", rune(character)) {
			continue
		}
		query := "SELECT lower" + string(character) + "('A')"
		if err := validateReadonlySQL(query); err == nil {
			t.Fatalf("unquoted control %x accepted", character)
		}
	}
	if err := validateReadonlySQL("SELECT 'literal\v\x01\r' AS value"); err != nil {
		t.Fatalf("quoted value must not be normalized or rejected: %v", err)
	}
}
