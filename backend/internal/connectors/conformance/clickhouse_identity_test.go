package conformance_test

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	clickhouse "github.com/aipermission/aipermission/backend/internal/connectors/clickhouse"
)

type identityResolver struct{ actions.ResolvedTarget }

func (r identityResolver) ResolveActionTarget(context.Context, string) (actions.ResolvedTarget, error) {
	return r.ResolvedTarget, nil
}

func TestMetadataIdentifiersSurviveSharedActionPreparation(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := registry.Register(clickhouse.New()); err != nil {
		t.Fatal(err)
	}
	service := actions.NewService(registry, identityResolver{actions.ResolvedTarget{
		Target:  connectors.TargetView{ID: 1, Ref: "clickhouse:1:2", ConnectorKind: clickhouse.Kind, Config: map[string]any{"database": "default"}},
		Profile: connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: clickhouse.Kind},
	}})
	for _, name := range []string{"events", " events ", " ", "`events`", `"events"`, `tenant\back`, "ending\\", "semi;colon", "line\nbreak"} {
		t.Run(name, func(t *testing.T) {
			for _, action := range []string{clickhouse.ActionGetTables, clickhouse.ActionDescribeTable} {
				input := map[string]any{"database": name}
				if action == clickhouse.ActionDescribeTable {
					input["table"] = name
				}
				prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
					Source: "mcp", TargetRef: "clickhouse:1:2", ActionName: action, Input: input, Reason: "verify exact metadata identity",
				})
				if err != nil {
					t.Fatal(err)
				}
				for key := range input {
					if prepared.Action.Payload[key] != name || prepared.Action.Preview[key] != name || prepared.Action.ContextMaterial[key] != name {
						t.Fatalf("%s changed %s=%q: %#v", action, key, name, prepared.Action)
					}
				}
			}
		})
	}
}

func TestMetadataIdentifiersRejectNULInsteadOfSelectingDefault(t *testing.T) {
	for _, key := range []string{"database", "table"} {
		input := map[string]any{"database": "default", "table": "events"}
		input[key] = "invalid\x00identifier"
		_, err := clickhouse.New().PrepareAction(context.Background(), connectors.ActionRequest{
			ActionName: clickhouse.ActionDescribeTable, Input: input,
			Target: connectors.TargetView{Config: map[string]any{"database": "fallback"}},
		})
		if err == nil {
			t.Fatalf("invalid %s was silently retargeted", key)
		}
	}
}
