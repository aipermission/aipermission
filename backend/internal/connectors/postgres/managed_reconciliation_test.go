package postgresconnector

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func managedReconciliationFixture(t *testing.T) (connectors.RuntimeContext, rolejournal.Entry, *recordedRoleStore, *recordedRoleSecrets) {
	t.Helper()
	runtime, _, store, secrets := managedLifecycleFixture(t)
	var record rolejournal.Record
	if err := json.Unmarshal([]byte(store.row.PublicData), &record); err != nil {
		t.Fatal(err)
	}
	record.Status = rolejournal.ProvisionIntent
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	store.row.PublicData = string(encoded)
	return runtime, rolejournal.Entry{ResourceID: store.row.ID, Record: record}, store, secrets
}

func TestManagedReconciliationRejectsChangedAuthorityBeforeAuditOrSecrets(t *testing.T) {
	for _, mode := range []string{"nil context", "wrong target", "wrong profile kind", "missing callback", "missing journal", "changed admin", "stale generation", "terminal record"} {
		t.Run(mode, func(t *testing.T) {
			runtime, expected, _, secrets := managedReconciliationFixture(t)
			ctx, audits := t.Context(), 0
			callback := func(context.Context) error { audits++; return errors.New("unexpected audit") }
			switch mode {
			case "nil context":
				ctx = nil
			case "wrong target":
				runtime.Target.ConnectorKind = "fixture"
			case "wrong profile kind":
				runtime.Profile.Kind = "fixture"
			case "missing callback":
				callback = nil
			case "missing journal":
				runtime.Capabilities = nil
			case "changed admin":
				runtime.Profile.ID++
			case "stale generation":
				expected.Record.Generation = expected.Record.Intent.OperationID
			case "terminal record":
				expected.Record.Status = rolejournal.Cleaned
			}
			got, err := New().ReconcileManagedRole(ctx, runtime, expected, callback)
			if err == nil || got != (rolejournal.Entry{}) || audits != 0 || secrets.reads != 0 {
				t.Fatalf("invalid request escaped admission: got=%#v err=%v audits=%d secrets=%d", got, err, audits, secrets.reads)
			}
		})
	}
}

func TestManagedReconciliationAuditHasAttachedDeadlineAndPreventsDial(t *testing.T) {
	for _, mode := range []string{"audit failed", "request canceled", "request deadline"} {
		t.Run(mode, func(t *testing.T) {
			runtime, expected, store, secrets := managedReconciliationFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "request deadline" {
				var timeoutCancel context.CancelFunc
				ctx, timeoutCancel = context.WithTimeout(ctx, 25*time.Millisecond)
				defer timeoutCancel()
			}
			failure, audits := errors.New("required audit unavailable"), 0
			before := store.row
			got, err := New().ReconcileManagedRole(ctx, runtime, expected, func(auditCtx context.Context) error {
				audits++
				deadline, bounded := auditCtx.Deadline()
				if !bounded || time.Until(deadline) <= 0 || time.Until(deadline) > 20*time.Second {
					t.Fatal("pre-connect audit lost lifecycle deadline")
				}
				switch mode {
				case "request canceled":
					cancel()
					<-auditCtx.Done()
					return auditCtx.Err()
				case "request deadline":
					<-auditCtx.Done()
					return auditCtx.Err()
				default:
					return failure
				}
			})
			if err == nil || got != (rolejournal.Entry{}) || audits != 1 || secrets.reads != 0 || store.row != before {
				t.Fatalf("audit failure dialed or changed journal: got=%#v err=%v audits=%d secrets=%d", got, err, audits, secrets.reads)
			}
			if mode == "audit failed" && !errors.Is(err, failure) {
				t.Fatalf("audit failure lost: %v", err)
			}
		})
	}
}
