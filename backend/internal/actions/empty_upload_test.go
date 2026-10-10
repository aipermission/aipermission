package actions_test

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
)

type emptyUploadTarget struct{}

func (emptyUploadTarget) ResolveActionTarget(context.Context, string) (actions.ResolvedTarget, error) {
	return actions.ResolvedTarget{
		Target: connectors.TargetView{ID: 1, Ref: "s3:1:1", ConnectorKind: "s3", Config: map[string]any{
			"host": "127.0.0.1", "port": 9000, "bucket": "test-bucket", "region": "us-east-1", "trust_conditional_requests": true,
		}},
		Profile: connectors.CredentialProfileView{ID: 1, TargetID: 1, ConnectorKind: "s3"},
	}, nil
}

func TestGatewayPreservesExplicitEmptyUpload(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := registry.Register(s3connector.New()); err != nil {
		t.Fatal(err)
	}
	service := actions.NewService(registry.Snapshot(), emptyUploadTarget{})
	for _, tc := range []struct {
		input map[string]any
		valid bool
	}{
		{map[string]any{"content_text": ""}, true},
		{map[string]any{"content_base64": ""}, true},
		{nil, false},
		{map[string]any{"content_text": "", "content_base64": ""}, false},
	} {
		input := map[string]any{"key": "empty.txt"}
		for key, value := range tc.input {
			input[key] = value
		}
		prepared, err := service.Prepare(t.Context(), actions.PrepareRequest{
			TargetRef: "s3:1:1", ActionName: "upload_object", Input: input, Reason: "test gateway normalization",
		})
		if (err == nil) != tc.valid {
			t.Fatalf("input=%#v err=%v", input, err)
		}
		if err == nil && (prepared.Action.Payload["content_bytes"] != 0 || prepared.Action.Payload["content_base64"] != "") {
			t.Fatalf("empty payload not preserved: %#v", prepared.Action.Payload)
		}
	}
}
