package connectortransport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestNativeTransportSnapshotDriftPreventsDispatch(t *testing.T) {
	cases := []struct {
		name   string
		change string
		allow  bool
	}{
		{"unchanged", "", true},
		{"local-without-snapshot", "nil", true},
		{"empty-approval", "empty", false},
		{"wrong-purpose", "purpose", false},
		{"config-drift", `UPDATE connector_targets SET config_json = '{"endpoint":"changed"}' WHERE id = ?`, false},
		{"public-drift", `UPDATE connector_credential_profiles SET public_json = '{"username":"changed"}' WHERE target_id = ?`, false},
		{"secret-fingerprint-drift", `UPDATE connector_credential_profiles SET encrypted_secret_json = 'rotated-opaque-ciphertext' WHERE target_id = ?`, false},
		{"target-archived", `UPDATE connector_targets SET status = 'archived' WHERE id = ?`, false},
		{"profile-archived", `UPDATE connector_credential_profiles SET status = 'archived' WHERE target_id = ?`, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNativeTransportFixture(t)
			approved := fixture.approved(t, connectors.CommandTransportCapabilityName)
			_, targetID, _, _ := connectors.ParseTargetRef(fixture.carrier)
			switch test.change {
			case "", "nil":
				if test.change == "nil" {
					approved = nil
				}
			case "empty":
				approved = NewApproved(nil)
			case "purpose":
				approved = fixture.approved(t, connectors.NetworkTransportCapabilityName)
			default:
				// Fault injection preserves timestamps, isolating each snapshot field
				// from revision-only comparisons and normal lifecycle side effects.
				result, err := fixture.database.ExecContext(t.Context(), test.change, targetID)
				if err != nil {
					t.Fatal(err)
				}
				if affected, err := result.RowsAffected(); err != nil || affected != 1 {
					t.Fatalf("snapshot fault was not applied: rows=%d error=%v", affected, err)
				}
			}
			lookups, calls := 0, 0
			adapter := commandAdapterFunc(func(context.Context, connectorapi.PeerIdentityGateway, connectorapi.LiveConsoleRuntime, string, string) (connectors.CommandRunResult, error) {
				calls++
				fixture.requireDeliveryHeld(t)
				return connectors.CommandRunResult{Stdout: "fixture-result", DispatchStarted: true}, nil
			})
			transport := Command{Approved: approved, Dependencies: Dependencies{Runtime: fixture.runtime, AdapterFor: func(string) connectorapi.Adapter {
				lookups++
				return adapter
			}}}
			result, err := transport.RunConnectorCommand(t.Context(), connectors.CommandRunRequest{
				SourceTargetRef: fixture.source, TransportTargetRef: fixture.carrier, Command: "fixture-command",
			})
			if test.allow {
				if err != nil || calls != 1 || lookups != 1 || result.Stdout != "fixture-result" || !result.DispatchStarted {
					t.Fatalf("positive dispatch: result=%#v err=%v calls=%d lookups=%d", result, err, calls, lookups)
				}
			} else if !errors.Is(err, ErrApprovalChanged) || calls != 0 || lookups != 0 || result != (connectors.CommandRunResult{}) {
				t.Fatalf("drift dispatched: result=%#v err=%v calls=%d lookups=%d", result, err, calls, lookups)
			}
			fixture.requireQuiescent(t)
		})
	}
}

func TestNativeHeldAdmissionStillValidatesSnapshot(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	approved := fixture.approved(t, connectors.CommandTransportCapabilityName)
	releaseExclusive, err := fixture.delivery.AcquireExclusive(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseExclusive()
	ctx := connectors.WithDeliveryAdmission(t.Context(), fixture.delivery.AdmissionIdentity())
	release, err := approved.Acquire(ctx, fixture.runtime, connectors.CommandTransportCapabilityName, fixture.carrier)
	if err != nil || release == nil {
		t.Fatalf("exact admission was reacquired: release=%t err=%v", release != nil, err)
	}
	release()
	_, targetID, _, _ := connectors.ParseTargetRef(fixture.carrier)
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE connector_targets SET name = 'changed' WHERE id = ?`, targetID); err != nil {
		t.Fatal(err)
	}
	if release, err := approved.Acquire(ctx, fixture.runtime, connectors.CommandTransportCapabilityName, fixture.carrier); release != nil || !errors.Is(err, ErrApprovalChanged) {
		t.Fatalf("held admission bypassed drift: release=%t err=%v", release != nil, err)
	}
	foreign := connectors.WithDeliveryAdmission(t.Context(), &connectors.DeliveryAdmissionIdentity{})
	foreign, cancel := context.WithTimeout(foreign, 20*time.Millisecond)
	defer cancel()
	if release, err := (Approved)(nil).Acquire(foreign, fixture.runtime, connectors.CommandTransportCapabilityName, fixture.carrier); release != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("foreign admission bypassed owner: release=%t err=%v", release != nil, err)
	}
	releaseExclusive()
	fixture.requireQuiescent(t)
}
