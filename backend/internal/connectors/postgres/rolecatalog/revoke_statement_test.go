package rolecatalog

import (
	"strings"
	"testing"
)

func TestRevokeStatementSupportsOnlyQuotedPrivilegeTargets(t *testing.T) {
	for _, kind := range []string{"database", "schema", "language", "foreign data wrapper", "foreign server", "routines"} {
		if sql, err := revokeStatement(kind, "", `odd"name`, `"role"`); err != nil || !strings.Contains(sql, `"odd""name"`) {
			t.Fatalf("%s quoting: %q %v", kind, sql, err)
		}
	}
	for _, kind := range []string{"table", "sequence", "type"} {
		if sql, err := revokeStatement(kind, `odd"schema`, " object ", `"role"`); err != nil || !strings.Contains(sql, `"odd""schema"." object "`) {
			t.Fatalf("%s quoting: %q %v", kind, sql, err)
		}
	}
	if sql, err := revokeStatement("large object", "", "4294967295", `"role"`); err != nil || !strings.Contains(sql, "LARGE OBJECT 4294967295") {
		t.Fatalf("large object: %q %v", sql, err)
	}
	for _, target := range [][3]string{
		{"table", "", "object"}, {"database", "public", "object"}, {"routines", "public", "object"},
		{"unknown", "", "object"}, {"table", "public", ""}, {"table", "public", "NUL\x00"},
		{"table", "\xff", "object"}, {"table", strings.Repeat("s", 64), "object"},
		{"large object", "", "0"}, {"large object", "", "01"}, {"large object", "", "4294967296"},
		{"large object", "public", "12"}, {"large object", "", "12;DROP TABLE data"},
	} {
		if sql, err := revokeStatement(target[0], target[1], target[2], `"role"`); err == nil || sql != "" {
			t.Fatalf("invalid privilege target accepted: %#v %q %v", target, sql, err)
		}
	}
}
