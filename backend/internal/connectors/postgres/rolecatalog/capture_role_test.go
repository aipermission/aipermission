package rolecatalog

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func TestCreatedRoleOIDRequiresExactUnboundDurableIntent(t *testing.T) {
	record := testRecord()
	entry := rolejournal.Entry{ResourceID: 1, Record: record}
	entry.Record.Status, entry.Record.RoleOID = rolejournal.ProvisionIntent, 0
	tx := validTransaction(t, record)
	if oid, err := CreatedRoleOID(t.Context(), tx, entry); err != nil || oid != 42 {
		t.Fatalf("created role identity=%d %v", oid, err)
	}
	for name, mutate := range map[string]func(*rolejournal.Entry){
		"record":        func(e *rolejournal.Entry) { e.Record.Version = 0 },
		"resource":      func(e *rolejournal.Entry) { e.ResourceID = 0 },
		"bound":         func(e *rolejournal.Entry) { e.Record.RoleOID = 42 },
		"status":        func(e *rolejournal.Entry) { e.Record.Status = rolejournal.RolledBack },
		"cluster":       func(e *rolejournal.Entry) { e.Record.Intent.Anchor.ClusterID = "123" },
		"database name": func(e *rolejournal.Entry) { e.Record.Intent.Anchor.DatabaseName = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := entry
			mutate(&changed)
			if oid, err := CreatedRoleOID(t.Context(), tx, changed); err == nil || oid != 0 {
				t.Fatal("invalid creation evidence accepted")
			}
		})
	}
	for _, mode := range []string{"zero OID", "successor", "name", "marker", "read failure"} {
		t.Run(mode, func(t *testing.T) {
			tx := validTransaction(t, record)
			valid := tx.rows[roleQuery]
			tx.rows[roleQuery] = func(destination ...any) error {
				if mode == "read failure" {
					return errors.New("read failed")
				}
				if err := valid(destination...); err != nil {
					return err
				}
				switch mode {
				case "zero OID":
					*destination[0].(*uint32) = 0
				case "successor":
					*destination[0].(*uint32) = record.Intent.Anchor.SuccessorOID
				case "name":
					*destination[1].(*string) = "other"
				case "marker":
					*destination[2].(*string) = "copied from another intent"
				}
				return nil
			}
			if oid, err := CreatedRoleOID(t.Context(), tx, entry); err == nil || oid != 0 {
				t.Fatal("wrong created role accepted")
			}
		})
	}
}
