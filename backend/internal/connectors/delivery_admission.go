package connectors

import "context"

// DeliveryAdmissionIdentity is an opaque, workspace-scoped delivery
// coordinator identity. Its non-zero size matters because distinct pointers to
// zero-size values may compare equal in Go.
type DeliveryAdmissionIdentity struct{ marker byte }

type deliveryAdmissionContextKey struct{}

// WithDeliveryAdmission records that ctx already owns admission for identity.
func WithDeliveryAdmission(ctx context.Context, identity *DeliveryAdmissionIdentity) context.Context {
	if identity == nil {
		return ctx
	}
	held, _ := ctx.Value(deliveryAdmissionContextKey{}).(map[*DeliveryAdmissionIdentity]struct{})
	next := make(map[*DeliveryAdmissionIdentity]struct{}, len(held)+1)
	for existing := range held {
		next[existing] = struct{}{}
	}
	next[identity] = struct{}{}
	return context.WithValue(ctx, deliveryAdmissionContextKey{}, next)
}

// DeliveryAdmissionHeld reports whether ctx already owns admission for identity.
func DeliveryAdmissionHeld(ctx context.Context, identity *DeliveryAdmissionIdentity) bool {
	if ctx == nil || identity == nil {
		return false
	}
	held, _ := ctx.Value(deliveryAdmissionContextKey{}).(map[*DeliveryAdmissionIdentity]struct{})
	_, ok := held[identity]
	return ok
}
