package actioncapacity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestNativeTokenScopeAndTerminalReservation(t *testing.T) {
	database, tokenID, foreignID, ids := capacityFixture(t)
	base, err := actioncapacity.Measure(t.Context(), database, tokenID, 0)
	if err != nil || base.Rows != 5 || base.Running != 1 || base.Bytes <= 2*(6<<20) {
		t.Fatalf("base usage=%#v err=%v", base, err)
	}
	for _, test := range []struct {
		name     string
		incoming int64
		extra    int64
	}{
		{name: "running counted once", incoming: ids[connectors.ResultRunning]},
		{name: "pending counted once", incoming: ids[connectors.ResultApprovalPending]},
		{name: "completed incoming", incoming: ids[connectors.ResultCompleted], extra: 6 << 20},
		{name: "foreign incoming", incoming: foreignID},
		{name: "missing incoming", incoming: 99999},
	} {
		t.Run(test.name, func(t *testing.T) {
			usage, err := actioncapacity.Measure(t.Context(), database, tokenID, test.incoming)
			if err != nil || usage.Rows != base.Rows || usage.Running != base.Running || usage.Bytes != base.Bytes+test.extra {
				t.Fatalf("usage=%#v base=%#v extra=%d err=%v", usage, base, test.extra, err)
			}
		})
	}
	missing, err := actioncapacity.Measure(t.Context(), database, tokenID+1, 0)
	if err != nil || missing != (actioncapacity.Usage{}) {
		t.Fatalf("exact missing-token identity leaked usage: %#v %v", missing, err)
	}
}

func TestNativeLimitsRejectEachExceededAxis(t *testing.T) {
	database, tokenID, _, _ := capacityFixture(t)
	usage, err := actioncapacity.Measure(t.Context(), database, tokenID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		limits  actioncapacity.Limits
		allowed bool
	}{
		{name: "exact", limits: actioncapacity.Limits{Rows: usage.Rows, Bytes: usage.Bytes, Running: usage.Running}, allowed: true},
		{name: "row", limits: actioncapacity.Limits{Rows: usage.Rows - 1, Bytes: usage.Bytes, Running: usage.Running}},
		{name: "byte", limits: actioncapacity.Limits{Rows: usage.Rows, Bytes: usage.Bytes - 1, Running: usage.Running}},
		{name: "running", limits: actioncapacity.Limits{Rows: usage.Rows, Bytes: usage.Bytes, Running: usage.Running - 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			allowed, err := actioncapacity.Within(t.Context(), database, tokenID, 0, test.limits)
			if err != nil || allowed != test.allowed {
				t.Fatalf("allowed=%v err=%v", allowed, err)
			}
		})
	}
	if actioncapacity.MaxRows != 20000 || actioncapacity.MaxBytes != 256<<20 || actioncapacity.MaxRunning != 4 || actioncapacity.TerminalReservationBytes != 6<<20 {
		t.Fatal("production capacity contract changed")
	}
	allowed, err := actioncapacity.WithinDefault(t.Context(), database, tokenID, 0)
	if err != nil || !allowed {
		t.Fatalf("default capacity=%v err=%v", allowed, err)
	}
}

func TestNativeCanceledMeasurementCannotAuthorizeCapacity(t *testing.T) {
	database, tokenID, _, _ := capacityFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	allowed, err := actioncapacity.WithinDefault(ctx, database, tokenID, 0)
	if allowed || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query authorized capacity: allowed=%v err=%v", allowed, err)
	}
}
