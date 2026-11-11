package actions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type failingRevalidationTokenReader struct{ err error }

func (reader failingRevalidationTokenReader) Get(context.Context, int64, time.Time) (AuthorizationToken, error) {
	return AuthorizationToken{}, reader.err
}

func TestRevalidationPreservesOperationalCausesWithoutExposingRawErrors(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := registry.Register(&prepareConnector{kind: "fixture"}); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{
		target:  connectors.TargetView{ID: 1, Ref: "fixture:1:1", ConnectorKind: "fixture"},
		profile: connectors.CredentialProfileView{ID: 1, TargetID: 1, ConnectorKind: "fixture"},
	}
	service := NewService(registry, resolver)
	prepared, err := service.Prepare(t.Context(), PrepareRequest{TargetRef: "fixture:1:1", ActionName: "query_readonly", Input: map[string]any{"sql": "select 1"}})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("private dependency failure sentinel")
	runtime := &Runtime{service: service, tokens: failingRevalidationTokenReader{err: cause}, mcpStarted: func() bool { return true }, now: time.Now}
	tokenID := int64(1)
	request := connectortargets.ActionRequest{TokenID: &tokenID, Source: SourceMCP}
	for _, targetFailure := range []bool{true, false} {
		resolver.err = nil
		if targetFailure {
			resolver.err = cause
		}
		_, err := runtime.Revalidate(t.Context(), request, prepared, connectortargets.ActionPermissionAlwaysRun)
		if !errors.Is(err, cause) || !errors.Is(err, ErrConnectorAuthorizationChanged) || strings.Contains(err.Error(), cause.Error()) {
			t.Fatalf("operational cause lost or exposed: target_failure=%v err=%v", targetFailure, err)
		}
	}
}
