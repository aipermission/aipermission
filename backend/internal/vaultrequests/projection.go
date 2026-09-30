package vaultrequests

import (
	"context"
	"fmt"
	"maps"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
)

type ProjectionRedactor func(context.Context, any) (any, error)
type RequestSealer func(int64, ExecutionEnvelope) (string, error)
type RequestOpener func(int64, string) (ExecutionEnvelope, error)

func RedactProjection(value any, redactText func(string) string) (any, error) {
	// Vault action objects use closed schemas. Their keys are protocol fields,
	// not caller-provided labels; changing them breaks authorization and decoding.
	redacted, err := actionresult.CanonicalizeAndRedact(value, actionresult.DefaultLimits(), actionresult.RedactionOptions{
		RedactText: redactText,
	})
	if err != nil {
		return nil, fmt.Errorf("redact Vault action projection: %w", err)
	}
	return redacted, nil
}

func (r *Runtime) publicApprovalContext(ctx context.Context, exact map[string]any) (map[string]any, error) {
	// Hashes, action constants, peer identities and ownership fields are machine
	// context. Only item names are user-controlled display metadata here.
	public := maps.Clone(exact)
	if items, ok := exact["items"]; ok {
		redacted, err := r.redactProjection(ctx, items)
		if err != nil {
			return nil, err
		}
		public["items"] = redacted
	}
	return public, nil
}

func (r *Runtime) publicProjection(ctx context.Context, input map[string]any, reason string) (map[string]any, string, error) {
	inputMap, err := r.publicObject(ctx, input)
	if err != nil {
		return nil, "", err
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

func (r *Runtime) publicObject(ctx context.Context, value map[string]any) (map[string]any, error) {
	redacted, err := r.redactProjection(ctx, nonNilMap(value))
	if err != nil {
		return nil, err
	}
	object, ok := redacted.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("redacted Vault action projection is not an object")
	}
	return object, nil
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
