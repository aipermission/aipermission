package postgresconnector

import (
	"strings"
	"testing"
)

func TestProvisionDOBlockPreservesBodyWithoutDelimiterCollision(t *testing.T) {
	for _, body := range []string{"BEGIN NULL; END", "BEGIN $$ $aipermission$ $aipermission_1$ $aipermission_2$ END"} {
		statement := provisionDOBlock(body)
		tagEnd := strings.Index(statement[4:], "$") + 5
		tag := statement[3:tagEnd]
		if strings.Contains(body, tag) || strings.Count(statement, tag) != 2 || statement != "DO "+tag+body+tag {
			t.Fatalf("PL/pgSQL body can escape its delimiter: %q", statement)
		}
		if provisionDOBlock(body) != statement {
			t.Fatal("grant SQL delimiter is nondeterministic")
		}
	}
}

func TestProvisionScopedWriteGrantUsesCollisionFreeDOBlock(t *testing.T) {
	filter := "ns.nspname = " + quoteLiteral("s$$;$aipermission$") + " AND tbl.relname = " + quoteLiteral("t$aipermission_1$")
	statement := provisionOwnedSequenceGrant("reader", filter)
	if !strings.HasPrefix(statement, "DO $aipermission_2$") || strings.Count(statement, "$aipermission_2$") != 2 || !strings.Contains(statement, filter) {
		t.Fatalf("write scope was rewritten or can terminate PL/pgSQL body: %q", statement)
	}
}
