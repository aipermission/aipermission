package connectors

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
)

func TestEffectiveRetryPolicyUsesConservativeDefaults(t *testing.T) {
	read := EffectiveRetryPolicy(ActionDefinition{Risk: RiskRead})
	if read.Class != RetryReadOnly || read.Guidance == "" {
		t.Fatalf("read policy = %#v", read)
	}
	write := EffectiveRetryPolicy(ActionDefinition{Risk: RiskWrite})
	if write.Class != RetryNonIdempotent || !strings.Contains(write.Guidance, "Do not retry automatically") {
		t.Fatalf("write policy = %#v", write)
	}
}

func TestNormalizePersistedRetryPolicyFailsClosed(t *testing.T) {
	for _, policy := range []RetryPolicy{{}, {Class: RetryClass("legacy_unknown")}} {
		normalized := NormalizePersistedRetryPolicy(policy)
		if normalized.Class != RetryNonIdempotent || normalized.Guidance == "" {
			t.Fatalf("normalized policy = %#v", normalized)
		}
	}
	read := NormalizePersistedRetryPolicy(RetryPolicy{Class: RetryReadOnly})
	if read.Class != RetryReadOnly || read.Guidance == "" {
		t.Fatalf("read policy = %#v", read)
	}
}

func TestGetActionDefinitionsCompletesWithoutMutatingConnectorCatalog(t *testing.T) {
	catalog := []ActionDefinition{{Name: "read", Label: "Read", Description: "Read data.", Risk: RiskRead}}
	connector := retryTestConnector{actions: catalog}
	actions, err := GetActionDefinitions(context.Background(), connector, TargetView{}, CredentialProfileView{})
	if err != nil {
		t.Fatalf("get definitions: %v", err)
	}
	if actions[0].RetryPolicy.Class != RetryReadOnly || actions[0].RetryPolicy.Guidance == "" {
		t.Fatalf("completed action = %#v", actions[0])
	}
	if !reflect.DeepEqual(catalog[0].RetryPolicy, RetryPolicy{}) {
		t.Fatalf("source catalog mutated: %#v", catalog[0])
	}
}

func TestValidateActionDefinitionsRejectsInvalidRetryContracts(t *testing.T) {
	base := ActionDefinition{Name: "write", Label: "Write", Description: "Write data.", Risk: RiskWrite, InputSchema: Schema{Fields: []Field{{Name: "etag", Label: "ETag", Type: FieldString}}}}

	invalidClass := base
	invalidClass.RetryPolicy = RetryPolicy{Class: RetryClass("sometimes")}
	if err := ValidateActionDefinitions([]ActionDefinition{invalidClass}, "test"); err == nil || !strings.Contains(err.Error(), "unsupported retry class") {
		t.Fatalf("invalid class error = %v", err)
	}

	missingField := base
	missingField.RetryPolicy = RetryPolicy{Class: RetryConditional, PreconditionFields: []string{"missing"}}
	if err := ValidateActionDefinitions([]ActionDefinition{missingField}, "test"); err == nil || !strings.Contains(err.Error(), "not in its input schema") {
		t.Fatalf("missing field error = %v", err)
	}

	missingPrecondition := base
	missingPrecondition.RetryPolicy = RetryPolicy{Class: RetryConditional}
	if err := ValidateActionDefinitions([]ActionDefinition{missingPrecondition}, "test"); err == nil || !strings.Contains(err.Error(), "requires a precondition field") {
		t.Fatalf("missing precondition error = %v", err)
	}

	readOnlyMutation := base
	readOnlyMutation.RetryPolicy = RetryPolicy{Class: RetryReadOnly}
	if err := ValidateActionDefinitions([]ActionDefinition{readOnlyMutation}, "test"); err == nil || !strings.Contains(err.Error(), "cannot declare read_only") {
		t.Fatalf("read-only mutation error = %v", err)
	}

	conditionalRead := base
	conditionalRead.Risk = RiskRead
	conditionalRead.RetryPolicy = RetryPolicy{Class: RetryConditional, PreconditionFields: []string{"etag"}}
	if err := ValidateActionDefinitions([]ActionDefinition{conditionalRead}, "test"); err == nil || !strings.Contains(err.Error(), "read action") {
		t.Fatalf("conditional read error = %v", err)
	}
}

func TestConditionalRetryPolicyCopiesPreconditionFields(t *testing.T) {
	fields := []string{"etag"}
	policy := ConditionalRetryPolicy(fields...)
	fields[0] = "changed"
	if policy.Class != RetryConditional || !reflect.DeepEqual(policy.PreconditionFields, []string{"etag"}) {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestRetryableIdempotentOperationErrorIsConservative(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "server error", err: retryHTTPStatusError(503), want: true},
		{name: "request timeout", err: retryHTTPStatusError(408), want: true},
		{name: "rate limited", err: retryHTTPStatusError(429), want: true},
		{name: "client error", err: retryHTTPStatusError(400), want: false},
		{name: "network error", err: &net.DNSError{Err: "temporary", IsTemporary: true}, want: true},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline", err: context.DeadlineExceeded, want: false},
		{name: "ordinary error", err: errors.New("invalid response"), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := RetryableIdempotentOperationError(test.err); got != test.want {
				t.Fatalf("retryable = %v, want %v", got, test.want)
			}
		})
	}
}

type retryHTTPStatusError int

func (err retryHTTPStatusError) Error() string       { return "http status" }
func (err retryHTTPStatusError) HTTPStatusCode() int { return int(err) }

type retryTestConnector struct {
	actions []ActionDefinition
}

func (retryTestConnector) Kind() string                          { return "retry_test" }
func (retryTestConnector) Label() string                         { return "Retry test" }
func (retryTestConnector) Version() string                       { return "0.1" }
func (retryTestConnector) TargetSchema() Schema                  { return Schema{} }
func (retryTestConnector) CredentialSchemas() []CredentialSchema { return nil }
func (retryTestConnector) GetHelp(context.Context, TargetView) (ConnectorHelp, error) {
	return ConnectorHelp{}, nil
}
func (connector retryTestConnector) GetActionList(context.Context, TargetView, CredentialProfileView) ([]ActionDefinition, error) {
	return connector.actions, nil
}
func (retryTestConnector) PrepareAction(context.Context, ActionRequest) (PreparedAction, error) {
	return PreparedAction{}, nil
}
func (retryTestConnector) ExecuteAction(context.Context, RuntimeContext, PreparedAction) (ActionResult, error) {
	return ActionResult{}, nil
}
