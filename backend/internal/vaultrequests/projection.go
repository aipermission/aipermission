package vaultrequests

import (
	"context"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
)

type ProjectionRedactor func(context.Context, any) (any, error)
type RequestSealer func(int64, ExecutionEnvelope) (string, error)
type RequestOpener func(int64, string) (ExecutionEnvelope, error)

func RedactProjection(value any, redactText func(string) string) (any, error) {
	redacted, err := actionresult.CanonicalizeAndRedact(value, actionresult.DefaultLimits(), actionresult.RedactionOptions{
		RedactText: redactText,
		RedactKey:  redactText,
	})
	if err != nil {
		return nil, fmt.Errorf("redact Vault action projection: %w", err)
	}
	return redacted, nil
}

func (r *Runtime) publicProjection(ctx context.Context, input map[string]any, reason string) (map[string]any, string, error) {
	redactedInput, err := r.redactProjection(ctx, nonNilMap(input))
	if err != nil {
		return nil, "", err
	}
	inputMap, ok := redactedInput.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("redacted Vault action input is not an object")
	}
	redactedReason, err := r.redactProjection(ctx, reason)
	if err != nil {
		return nil, "", err
	}
	reasonText, ok := redactedReason.(string)
	if !ok {
		return nil, "", fmt.Errorf("redacted Vault action reason is not text")
	}
	return inputMap, reasonText, nil
}

func (r *Runtime) exactRequest(item Request) (Request, error) {
	if r.openRequest == nil || item.ID < 1 || item.EncryptedPayloadJSON == "" {
		return Request{}, fmt.Errorf("Vault action request metadata is unavailable")
	}
	envelope, err := r.openRequest(item.ID, item.EncryptedPayloadJSON)
	if err != nil {
		return Request{}, fmt.Errorf("decrypt Vault action request metadata: %w", err)
	}
	item.Input = nonNilMap(envelope.Input)
	item.Reason = envelope.Reason
	item.ApprovalContext = nonNilMap(envelope.ApprovalContext)
	return item, nil
}
