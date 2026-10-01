package rolecatalog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func TestOperatorCleanupRequiresFreshProvisionedAuthorityBeforeDial(t *testing.T) {
	for _, mode := range []string{"nil context", "canceled", "missing journal", "stale generation", "changed authority", "provision intent", "cleanup intent", "cleaned", "rolled back"} {
		t.Run(mode, func(t *testing.T) {
			journal, store, _, connection := lifecycleFixture(t)
			entry := provisionFixture(t, journal, connection)
			before := store.last
			authority, ctx := entry.Record.Intent.Anchor, t.Context()
			switch mode {
			case "nil context":
				ctx = nil
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			case "missing journal":
				journal = nil
			case "stale generation":
				entry.Record.Generation = entry.Record.Intent.OperationID
			case "changed authority":
				authority.AdminProfileID++
			default:
				status := map[string]rolejournal.Status{"provision intent": rolejournal.ProvisionIntent, "cleanup intent": rolejournal.CleanupIntent, "cleaned": rolejournal.Cleaned, "rolled back": rolejournal.RolledBack}[mode]
				store.last.Status, entry.Record.Status = status, status
				encoded, err := json.Marshal(store.last)
				if err != nil {
					t.Fatal(err)
				}
				store.row.PublicData = string(encoded)
				before = store.last
			}
			calls := 0
			got, err := CleanupConfirmed(ctx, journal, entry, authority, func(context.Context) (Connection, error) {
				calls++
				return connection, nil
			})
			if err == nil || got != (rolejournal.Entry{}) || calls != 0 || store.last != before {
				t.Fatalf("unsafe operator cleanup: got=%#v err=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestOperatorCleanupHandlesOrphanWithoutLocalCredentialAndNeverReplaysUnknownCommit(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "unknown"}[unknown], func(t *testing.T) {
			journal, store, tx, connection := lifecycleFixture(t)
			entry := provisionFixture(t, journal, connection)
			tx.statements, tx.commits = nil, 0
			if unknown {
				tx.commitErr = errors.New("lost cleanup commit acknowledgement")
			}
			calls := 0
			dial := func(context.Context) (Connection, error) { calls++; return connection, nil }
			got, err := CleanupConfirmed(t.Context(), journal, entry, entry.Record.Intent.Anchor, dial)
			if unknown {
				if err == nil || connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || got != (rolejournal.Entry{}) || store.last.Status != rolejournal.CleanupIntent {
					t.Fatalf("lost acknowledgement was not fenced: %#v %v", got, err)
				}
			} else if err != nil || got.Record.Status != rolejournal.Cleaned || got.Reference() != entry.Reference() {
				t.Fatalf("orphan cleanup failed: %#v %v", got, err)
			}
			if calls != 1 || tx.commits != 1 || len(mutationStatements(tx)) == 0 {
				t.Fatal("operator cleanup did not use the transaction plan")
			}
			if _, err := CleanupConfirmed(t.Context(), journal, entry, entry.Record.Intent.Anchor, dial); !errors.Is(err, rolejournal.ErrStaleGeneration) || calls != 1 {
				t.Fatalf("stale cleanup decision repeated remote dispatch: %v calls=%d", err, calls)
			}
		})
	}
}
