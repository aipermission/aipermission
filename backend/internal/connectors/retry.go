package connectors

import (
	"context"
	"errors"
	"net"
)

// RetryClass describes whether a connector action may be repeated after an
// uncertain result. The gateway idempotency key deduplicates local request
// submission; it does not prove whether an external mutation took effect.
type RetryClass string

const (
	RetryReadOnly      RetryClass = "read_only"
	RetryIdempotent    RetryClass = "idempotent"
	RetryConditional   RetryClass = "conditional"
	RetryNonIdempotent RetryClass = "non_idempotent"
)

// RetryPolicy is connector-owned, machine-readable retry guidance. An empty
// policy receives a conservative default based on the action risk.
type RetryPolicy struct {
	Class              RetryClass `json:"class"`
	PreconditionFields []string   `json:"precondition_fields,omitempty"`
	Guidance           string     `json:"guidance"`
}

// EffectiveRetryPolicy returns a complete policy for API, MCP, approval
// snapshots, and UI consumers.
func EffectiveRetryPolicy(action ActionDefinition) RetryPolicy {
	policy := action.RetryPolicy
	if policy.Class == "" {
		if action.Risk == RiskRead {
			policy.Class = RetryReadOnly
		} else {
			policy.Class = RetryNonIdempotent
		}
	}
	if policy.Guidance == "" {
		policy.Guidance = retryGuidance(policy.Class)
	}
	return policy
}

// NormalizePersistedRetryPolicy converts pre-contract or incomplete stored
// values to the conservative mutation default used by current API surfaces.
func NormalizePersistedRetryPolicy(policy RetryPolicy) RetryPolicy {
	if !validRetryClass(policy.Class) {
		return EffectiveRetryPolicy(ActionDefinition{Risk: RiskWrite})
	}
	return EffectiveRetryPolicy(ActionDefinition{Risk: RiskWrite, RetryPolicy: policy})
}

// GetActionDefinitions resolves, completes, and validates one connector's
// action contract. Production callers use this boundary so every API surface
// observes identical retry metadata.
func GetActionDefinitions(ctx context.Context, connector Connector, target TargetView, profile CredentialProfileView) ([]ActionDefinition, error) {
	actions, err := connector.GetActionList(ctx, target, profile)
	if err != nil {
		return nil, err
	}
	completed := make([]ActionDefinition, len(actions))
	copy(completed, actions)
	for index := range completed {
		completed[index].RetryPolicy = EffectiveRetryPolicy(completed[index])
	}
	if err := ValidateActionDefinitions(completed, connector.Kind()+" actions"); err != nil {
		return nil, err
	}
	return completed, nil
}

func retryGuidance(class RetryClass) string {
	switch class {
	case RetryReadOnly:
		return "Inspect the recorded result first. Use the same idempotency key only to retrieve the same gateway submission; use a new key to start a new external attempt."
	case RetryIdempotent:
		return "Inspect external state after an unknown outcome. Use the same idempotency key only for the same gateway submission; use a new key for a new external attempt."
	case RetryConditional:
		return "After a precondition failure, refresh external state and use fresh preconditions plus a new idempotency key. After an unknown outcome, do not retry until external state proves the original mutation did not commit. Reuse the old key only to retrieve the original submission."
	default:
		return "Do not retry automatically after execution starts or the outcome becomes unknown; inspect external state first."
	}
}

// ConditionalRetryPolicy returns a per-request policy for a mutation whose
// prepared payload contains concrete optimistic-concurrency preconditions.
func ConditionalRetryPolicy(fields ...string) *RetryPolicy {
	return &RetryPolicy{Class: RetryConditional, PreconditionFields: append([]string(nil), fields...)}
}

type httpStatusError interface {
	HTTPStatusCode() int
}

// RetryableIdempotentOperationError recognizes transient transport failures for
// an operation whose remote identity is independently known to be idempotent.
// It must never be used to justify replaying an arbitrary mutation.
func RetryableIdempotentOperationError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		status := statusErr.HTTPStatusCode()
		return status == 408 || status == 429 || status >= 500
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}

func validRetryClass(class RetryClass) bool {
	switch class {
	case RetryReadOnly, RetryIdempotent, RetryConditional, RetryNonIdempotent:
		return true
	default:
		return false
	}
}
