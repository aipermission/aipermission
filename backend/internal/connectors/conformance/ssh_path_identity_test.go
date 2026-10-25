package conformance_test

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	ssh "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

type pathResolver struct{ actions.ResolvedTarget }

func (r pathResolver) ResolveActionTarget(context.Context, string) (actions.ResolvedTarget, error) {
	return r.ResolvedTarget, nil
}

func TestBrowsePathIdentitySurvivesSharedPreparation(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := registry.Register(ssh.New()); err != nil {
		t.Fatal(err)
	}
	service := actions.NewService(registry, pathResolver{actions.ResolvedTarget{
		Target:  connectors.TargetView{ID: 1, Ref: "ssh:1:2", ConnectorKind: ssh.Kind},
		Profile: connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: ssh.Kind},
	}})
	for _, value := range []string{" directory ", "/var/data/with-space ", " ", "~/ directory "} {
		prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
			Source: "mcp", TargetRef: "ssh:1:2", ActionName: ssh.ActionBrowseRemoteFiles,
			Input: map[string]any{"path": value}, Reason: "verify exact path",
		})
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Action.Payload["path"] != value || prepared.Action.Preview["path"] != value || prepared.Action.ContextMaterial["path"] != value {
			t.Fatalf("path identity changed: %#v", prepared.Action)
		}
	}
}

func TestDownloadPathsRejectMalformedSelectionsInsteadOfChangingThem(t *testing.T) {
	for _, input := range []any{"/file", []any{"/file", 1}, []any{"/file", ""}, []any{" "}, 1} {
		if _, err := ssh.New().PrepareAction(context.Background(), connectors.ActionRequest{
			ActionName: ssh.ActionStartFileDownload, Input: map[string]any{"remote_paths": input},
		}); err == nil {
			t.Fatalf("malformed paths accepted: %#v", input)
		}
	}
}
