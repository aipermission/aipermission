package connectortransport

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestUnapprovedTransportStillHoldsDeliveryAdmission(t *testing.T) {
	acquired := 0
	released := 0
	runtime := Runtime{
		Database: &sql.DB{},
		AcquireDelivery: func(context.Context) (func(), error) {
			acquired++
			return func() { released++ }, nil
		},
	}
	release, err := (Approved)(nil).Acquire(t.Context(), runtime, "command", "ssh:1:1")
	if err != nil {
		t.Fatal(err)
	}
	if acquired != 1 || released != 0 || release == nil {
		t.Fatalf("acquired=%d released=%d release=%v", acquired, released, release != nil)
	}
	release()
	if released != 1 {
		t.Fatalf("released=%d", released)
	}
}

func TestTransportAdmissionReleasesPartialAcquisition(t *testing.T) {
	released := 0
	want := errors.New("canceled")
	_, err := (Approved)(nil).Acquire(t.Context(), Runtime{
		Database: &sql.DB{},
		AcquireDelivery: func(context.Context) (func(), error) {
			return func() { released++ }, want
		},
	}, "command", "ssh:1:1")
	if !errors.Is(err, want) || released != 1 {
		t.Fatalf("error=%v released=%d", err, released)
	}
}

func TestTransportReusesExactLifecycleAdmission(t *testing.T) {
	coordinator := &vaultsessions.DeliveryCoordinator{}
	releaseExclusive, err := coordinator.AcquireExclusive(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseExclusive()

	ctx := connectors.WithDeliveryAdmission(t.Context(), coordinator.AdmissionIdentity())
	release, err := (Approved)(nil).Acquire(ctx, Runtime{
		Database: &sql.DB{}, AcquireDelivery: coordinator.AcquireDelivery,
		Admission: coordinator.AdmissionIdentity(),
	}, "network_transport", "ssh:1:1")
	if err != nil {
		t.Fatalf("nested transport reacquired exclusive admission: %v", err)
	}
	if release == nil {
		t.Fatal("nested transport returned no release function")
	}
	release()

	other := &connectors.DeliveryAdmissionIdentity{}
	timeoutCtx, cancel := context.WithTimeout(connectors.WithDeliveryAdmission(t.Context(), other), 20*time.Millisecond)
	defer cancel()
	if _, err := (Approved)(nil).Acquire(timeoutCtx, Runtime{
		Database: &sql.DB{}, AcquireDelivery: coordinator.AcquireDelivery,
		Admission: coordinator.AdmissionIdentity(),
	}, "network_transport", "ssh:1:1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("different admission identity bypassed coordinator: %v", err)
	}
}
