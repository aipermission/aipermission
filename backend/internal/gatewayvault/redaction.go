package gatewayvault

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type RequestProjectionRedactor func(context.Context, any) (any, error)

// PrepareRequestProjectionRedactor owns structured projection masking after
// policy has been captured, so effect transactions do not query policy storage.
func PrepareRequestProjectionRedactor(redact func(string) string) RequestProjectionRedactor {
	if redact == nil {
		return nil
	}
	return func(_ context.Context, value any) (any, error) {
		return vaultrequests.RedactProjection(value, redact)
	}
}

func RedactRequestProjection(ctx context.Context, value any, redact func(context.Context, string) string) (any, error) {
	return vaultrequests.RedactProjection(value, func(text string) string {
		return redact(ctx, text)
	})
}
