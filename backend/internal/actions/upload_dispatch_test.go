package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestUploadGrantExpiryUsesEarliestBound(t *testing.T) {
	const early = "2026-11-16T12:00:00.123Z"
	const late = "2026-11-16T13:00:00Z"
	for _, test := range []struct {
		name, snapshot, want string
		invalid              bool
	}{
		{"unbounded", `{}`, "", false},
		{"token", `{"token":{"expires_at":"` + early + `"}}`, early, false},
		{"permission", `{"permission":{"expires_at":"` + early + `"}}`, early, false},
		{"token-first", `{"token":{"expires_at":"` + early + `"},"permission":{"expires_at":"` + late + `"}}`, early, false},
		{"permission-first", `{"token":{"expires_at":"` + late + `"},"permission":{"expires_at":"` + early + `"}}`, early, false},
		{"malformed", `{`, "", true},
		{"invalid-token", `{"token":{"expires_at":"never"}}`, "", true},
		{"invalid-permission", `{"permission":{"expires_at":"never"}}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := uploadGrantExpiry(test.snapshot)
			if test.invalid {
				if err == nil {
					t.Fatal("malformed grant received unbounded authorization")
				}
				return
			}
			var want time.Time
			if test.want != "" {
				want, _ = time.Parse(time.RFC3339Nano, test.want)
			}
			if err != nil || !got.Equal(want) {
				t.Fatalf("expiry=%v error=%v want=%v", got, err, want)
			}
		})
	}
}

func TestUploadDispatchRejectsBrokenGrantAndUnavailableAdmission(t *testing.T) {
	tokenID := int64(1)
	blocked := errors.New("delivery unavailable")
	for _, snapshot := range []string{`{`, `{"token":{"expires_at":"2999-01-01T00:00:00Z"}}`} {
		delivery := &workflowTestDelivery{err: blocked}
		runtime := &Runtime{delivery: delivery}
		request := connectortargets.ActionRequest{Source: SourceMCP, TokenID: &tokenID, IdempotencyKey: "upload", ApprovalContext: snapshot}
		prepared := PreparedRequest{ActionDefinition: connectors.ActionDefinition{MaxUploadBytes: 1}}
		ctx := runtime.withUploadDispatch(t.Context(), request, prepared, ExecutionOptions{})
		authorize := connectors.UploadDispatchAuthorization(ctx, "upload")
		if authorize == nil || connectors.UploadDispatchAuthorization(ctx, "another-upload") != nil {
			t.Fatal("dispatch authorization lost its exact upload reference")
		}
		bound, release, err := authorize(t.Context())
		want, calls := ErrConnectorAuthorizationChanged, 0
		if snapshot != "{" {
			want, calls = blocked, 1
		}
		if !errors.Is(err, want) || bound != nil || release != nil || delivery.calls != calls {
			t.Fatalf("failed admission returned authority: bound=%v release=%v error=%v calls=%d", bound, release != nil, err, delivery.calls)
		}
	}
}

func TestNativeUploadDispatchRevalidatesUnderAdmission(t *testing.T) {
	f := newNativeWorkflowFixture(t, connectortargets.ActionPermissionAlwaysRun, connectors.RiskWrite)
	f.connector.actions[0].MaxUploadBytes = 1024
	var authorized bool
	f.connector.execute = func(ctx context.Context, _ connectors.RuntimeContext, _ connectors.PreparedAction) (connectors.ActionResult, error) {
		authorize := connectors.UploadDispatchAuthorization(ctx, f.call.IdempotencyKey)
		if authorize == nil {
			t.Fatal("staged dispatch omitted revalidation callback")
		}
		bound, release, err := authorize(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bound == nil || release == nil || !f.delivery.held() {
			t.Fatal("upload dispatch escaped delivery admission")
		}
		release()
		authorized = true
		return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
	}
	result, err := f.runtime.Call(connectors.WithStagedUpload(t.Context()), f.call)
	if err != nil || !authorized || result.Request.Status != connectors.ResultCompleted || f.delivery.held() {
		t.Fatalf("upload dispatch or lease release failed: status=%s authorized=%v error=%v", result.Request.Status, authorized, err)
	}
}
