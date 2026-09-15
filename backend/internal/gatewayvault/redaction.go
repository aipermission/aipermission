package gatewayvault

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func RedactRequestProjection(ctx context.Context, value any, redact func(context.Context, string) string) (any, error) {
	return vaultrequests.RedactProjection(value, func(text string) string {
		return redact(ctx, text)
	})
}
