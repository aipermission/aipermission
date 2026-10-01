package rolecatalog

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCleanupPlanCannotDropOwnedObjects(t *testing.T) {
	tx := cleanupTransaction(t, [3]string{"database", "", " Main DB "},
		[3]string{"table", " odd schema ", "table\"name"}, [3]string{"table", " odd schema ", "table\"name"})
	statements, err := CleanupPlan(t.Context(), tx, testRecord())
	if err != nil || len(statements) != 4 {
		t.Fatalf("cleanup statements=%#v error=%v", statements, err)
	}
	if statements[0] != `REASSIGN OWNED BY " My Role " TO " Main Admin "` ||
		statements[2] != `REVOKE ALL PRIVILEGES ON table " odd schema "."table""name" FROM " My Role " CASCADE` ||
		statements[3] != `DROP ROLE " My Role "` {
		t.Fatalf("exact quoted targets lost: %#v", statements)
	}
	for _, statement := range statements {
		if strings.Contains(statement, "DROP OWNED") {
			t.Fatal("cleanup plan can destroy late-owned objects")
		}
	}
	if len(tx.statements) != len(sharedOwnershipLocks) || !tx.queryRows.(*privilegeRows).closed {
		t.Fatal("planning mutated remote state or leaked rows")
	}
	if got := tx.arguments[2]; len(got) != 2 || got[0] != uint32(42) || got[1] != uint32(12) {
		t.Fatalf("scope check lost bound OIDs: %#v", got)
	}
}

func TestCleanupPlanFailsBeforeAnyMutationOrPartialPlan(t *testing.T) {
	for name, change := range map[string]func(*transaction){
		"role absent": func(tx *transaction) { tx.rows[roleQuery] = func(...any) error { return errors.New("role absent") } },
		"scope failure": func(tx *transaction) {
			tx.rows[cleanupScopeQuery] = func(...any) error { return errors.New("scope read failed") }
		},
		"other database or shared ownership": func(tx *transaction) {
			tx.rows[cleanupScopeQuery] = func(destination ...any) error { *destination[0].(*bool) = true; return nil }
		},
		"privilege query failure": func(tx *transaction) { tx.queryErr = errors.New("privilege query failed") },
		"rows missing":            func(tx *transaction) { tx.queryRows = nil },
		"scan failure":            func(tx *transaction) { tx.queryRows.(*privilegeRows).scanErr = errors.New("scan failed") },
		"iteration failure":       func(tx *transaction) { tx.queryRows.(*privilegeRows).err = errors.New("iteration failed") },
		"unsupported privilege":   func(tx *transaction) { tx.queryRows.(*privilegeRows).values[0][0] = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			tx := cleanupTransaction(t, [3]string{"table", "public", "fixture"})
			change(tx)
			if plan, err := CleanupPlan(t.Context(), tx, testRecord()); err == nil || plan != nil {
				t.Fatalf("unsafe partial plan=%#v error=%v", plan, err)
			}
			for _, sql := range tx.statements {
				if !strings.HasPrefix(sql, "SELECT") && !strings.HasPrefix(sql, "SET LOCAL") {
					t.Fatalf("planning performed a mutation: %q", sql)
				}
			}
		})
	}
}

func TestCleanupPlanEnforcesStatementAndAggregateByteBudgets(t *testing.T) {
	for _, mode := range []string{"statements", "bytes", "duplicate row count"} {
		t.Run(mode, func(t *testing.T) {
			targets := make([][3]string, maxCleanupStatements+1)
			for index := range targets {
				schema, name := "public", fmt.Sprintf("table%d", index)
				if mode == "bytes" {
					schema, name = strings.Repeat("s", 63), fmt.Sprintf("%060d", index)
				}
				if mode == "duplicate row count" {
					name = "duplicate"
				}
				targets[index] = [3]string{"table", schema, name}
			}
			tx := cleanupTransaction(t, targets...)
			if plan, err := CleanupPlan(t.Context(), tx, testRecord()); err == nil || plan != nil {
				t.Fatalf("%s budget accepted: %#v %v", mode, plan, err)
			}
			if !tx.queryRows.(*privilegeRows).closed {
				t.Fatal("budget failure leaked catalog rows")
			}
		})
	}
}
