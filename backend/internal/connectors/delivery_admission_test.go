package connectors

import (
	"context"
	"testing"
)

func TestDeliveryAdmissionIdentityIsExactAndComposable(t *testing.T) {
	first, second := &DeliveryAdmissionIdentity{}, &DeliveryAdmissionIdentity{}
	ctx := WithDeliveryAdmission(context.Background(), first)
	if !DeliveryAdmissionHeld(ctx, first) || DeliveryAdmissionHeld(ctx, second) {
		t.Fatal("admission identity was not isolated")
	}
	ctx = WithDeliveryAdmission(ctx, second)
	if !DeliveryAdmissionHeld(ctx, first) || !DeliveryAdmissionHeld(ctx, second) {
		t.Fatal("nested admission identities were not preserved")
	}
}

func TestNilDeliveryAdmissionIdentityDoesNotMarkContext(t *testing.T) {
	ctx := context.Background()
	if got := WithDeliveryAdmission(ctx, nil); got != ctx {
		t.Fatal("nil identity replaced the context")
	}
	if DeliveryAdmissionHeld(ctx, nil) {
		t.Fatal("nil identity must never be admitted")
	}
}
