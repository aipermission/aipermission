package conformance_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func testPostgresSQLExecutionSettings(t *testing.T) {
	for _, probe := range []struct{ name, setting, expression, want string }{
		{"date", "SET DateStyle='German';", "DATE '2020-01-31'", "31.01.2020"},
		{"date_sql", "SET DateStyle='SQL, DMY';", "DATE '2020-01-31'", "31/01/2020"},
		{"interval", "SET IntervalStyle='sql_standard';", "INTERVAL '1 year 2 months 3 days 4 hours'", "+1-2 +3 +4:00:00"},
		{"interval_verbose", "SET IntervalStyle='postgres_verbose';", "INTERVAL '1 day'", "@ 1 day"},
		{"bytea", "SET bytea_output='escape';", "'\\x01'::bytea", `\001`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			f := newPostgresExecutionFixture(t)
			script := fmt.Sprintf("%s UPDATE %s SET value=value+1; SELECT %s AS value; SELECT repeat('x',100000) FROM generate_series(1,12); UPDATE %s SET value=value+10;", probe.setting, f.table, probe.expression, f.table)
			out := runSQLExecution(t, f.sqlExecutionFixture, script, 1)
			encoded, _ := json.Marshal(out)
			quoted, _ := json.Marshal(probe.want)
			if f.value(t) != 11 || !strings.Contains(string(encoded), string(quoted)) || len(out.Notices) == 0 {
				t.Fatalf("session output settings interrupted mutations or lost server text: %s", encoded)
			}
		})
	}
	t.Run("client_encoding", func(t *testing.T) {
		f := newPostgresExecutionFixture(t)
		out := runSQLExecution(t, f.sqlExecutionFixture, fmt.Sprintf("SET client_encoding='LATIN1'; SELECT 'é' AS value; UPDATE %s SET value=11; SET client_encoding='UTF8'; SELECT 'é' AS restored;", f.table), 10)
		encoded, _ := json.Marshal(out)
		if f.value(t) != 11 || !out.Truncated || len(out.Notices) == 0 || strings.Contains(string(encoded), "�") || !strings.Contains(string(encoded), `"restored":"é"`) {
			t.Fatalf("non-UTF8 results silently changed or interrupted later commands: %s", encoded)
		}
	})
}
