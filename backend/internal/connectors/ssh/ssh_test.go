package sshconnector

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectorMetadataAndSchemas(t *testing.T) {
	connector := New()
	if connector.Kind() != Kind || connector.Label() != Label || connector.Version() == "" {
		t.Fatalf("unexpected metadata kind=%q label=%q version=%q", connector.Kind(), connector.Label(), connector.Version())
	}

	targetSchema := connector.TargetSchema()
	if !hasField(targetSchema, "host") || !hasField(targetSchema, "port") {
		t.Fatalf("expected host and port target fields, got %#v", targetSchema.Fields)
	}

	credentialSchemas := connector.CredentialSchemas()
	if len(credentialSchemas) != 1 || credentialSchemas[0].Kind != "private_key" {
		t.Fatalf("unexpected credential schemas: %#v", credentialSchemas)
	}
	if !hasField(credentialSchemas[0].Schema, "username") || !hasField(credentialSchemas[0].Schema, "private_key") {
		t.Fatalf("expected username and private_key credential fields, got %#v", credentialSchemas[0].Schema.Fields)
	}
}

func TestGetHelpAndActionList(t *testing.T) {
	connector := New()
	target := connectors.TargetView{Ref: "ssh:core", Name: "core-1", ConnectorKind: Kind}

	help, err := connector.GetHelp(context.Background(), target)
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if help.ConnectorID != Kind || help.Title == "" || len(help.Usage) == 0 {
		t.Fatalf("unexpected help: %#v", help)
	}

	actions, err := connector.GetActionList(context.Background(), target)
	if err != nil {
		t.Fatalf("action list: %v", err)
	}
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d", len(actions))
	}
	if actions[0].Name != ActionExec || actions[0].Risk != connectors.RiskWrite {
		t.Fatalf("unexpected exec action: %#v", actions[0])
	}
	if actions[1].Name != ActionReadConsole || actions[1].Risk != connectors.RiskRead {
		t.Fatalf("unexpected read_console action: %#v", actions[1])
	}
	if actions[2].Name != ActionRestartConsoleSession {
		t.Fatalf("unexpected restart action: %#v", actions[2])
	}
	if actions[3].Name != ActionBrowseRemoteFiles || actions[3].Risk != connectors.RiskRead {
		t.Fatalf("unexpected browse action: %#v", actions[3])
	}
	if actions[4].Name != ActionStartFileDownload || actions[4].Risk != connectors.RiskRead {
		t.Fatalf("unexpected download action: %#v", actions[4])
	}
	if actions[5].Name != ActionUploadFiles || actions[5].Risk != connectors.RiskWrite {
		t.Fatalf("unexpected upload action: %#v", actions[5])
	}
}

func TestPrepareExec(t *testing.T) {
	connector := New()
	prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", Name: "worker-2", ConnectorKind: Kind},
		Profile:    connectors.CredentialProfileView{ID: 42, ConnectorKind: Kind, Kind: "private_key", Label: "root"},
		ActionName: ActionExec,
		Input:      map[string]any{"command": " apt update "},
		Reason:     "maintenance",
	})
	if err != nil {
		t.Fatalf("prepare exec: %v", err)
	}
	if prepared.ConnectorKind != Kind || prepared.ActionName != ActionExec || prepared.ProfileID != 42 {
		t.Fatalf("unexpected prepared action: %#v", prepared)
	}
	if got := prepared.Payload["command"]; got != "apt update" {
		t.Fatalf("command payload = %#v", got)
	}
	if prepared.ContextMaterial["reason"] != "maintenance" {
		t.Fatalf("context material did not include reason: %#v", prepared.ContextMaterial)
	}
}

func TestPrepareExecRequiresCommand(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionExec,
		Input:      map[string]any{"command": "   "},
	})
	if err == nil {
		t.Fatal("expected missing command error")
	}
}

func TestPrepareReadConsoleClampsTail(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionReadConsole,
		Input:      map[string]any{"tail_bytes": float64(maxConsoleTailBytes + 5000)},
	})
	if err != nil {
		t.Fatalf("prepare read_console: %v", err)
	}
	if got := prepared.Payload["tail_bytes"]; got != maxConsoleTailBytes {
		t.Fatalf("tail_bytes = %#v, want %d", got, maxConsoleTailBytes)
	}
	if prepared.Risk != connectors.RiskRead {
		t.Fatalf("risk = %q", prepared.Risk)
	}
}

func TestPrepareRestartConsoleSession(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionRestartConsoleSession,
	})
	if err != nil {
		t.Fatalf("prepare restart: %v", err)
	}
	if prepared.Payload == nil || len(prepared.Payload) != 0 {
		t.Fatalf("expected empty payload map, got %#v", prepared.Payload)
	}
	if prepared.Risk != connectors.RiskWrite {
		t.Fatalf("risk = %q", prepared.Risk)
	}
}

func TestPrepareBrowseRemoteFilesDefaultsPath(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionBrowseRemoteFiles,
	})
	if err != nil {
		t.Fatalf("prepare browse: %v", err)
	}
	if got := prepared.Payload["path"]; got != "~" {
		t.Fatalf("path = %#v", got)
	}
}

func TestPrepareStartFileDownload(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionStartFileDownload,
		Input: map[string]any{
			"remote_paths": []any{"/var/log/syslog", "  /etc/hosts "},
			"archive_name": "logs.zip",
		},
	})
	if err != nil {
		t.Fatalf("prepare download: %v", err)
	}
	paths, ok := prepared.Payload["remote_paths"].([]string)
	if !ok || len(paths) != 2 || paths[1] != "/etc/hosts" {
		t.Fatalf("remote_paths = %#v", prepared.Payload["remote_paths"])
	}
	if prepared.Preview["items"] != 2 {
		t.Fatalf("items preview = %#v", prepared.Preview["items"])
	}
}

func TestPrepareStartFileDownloadRequiresRemotePaths(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionStartFileDownload,
		Input:      map[string]any{"remote_paths": []any{"  "}},
	})
	if err == nil {
		t.Fatal("expected missing remote_paths error")
	}
}

func TestPrepareUploadFiles(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionUploadFiles,
		Input: map[string]any{
			"local_paths": []string{"/tmp/report.txt", "/tmp/log.txt"},
			"remote_dir":  " /home/root ",
			"overwrite":   "true",
		},
	})
	if err != nil {
		t.Fatalf("prepare upload: %v", err)
	}
	if got := prepared.Payload["remote_dir"]; got != "/home/root" {
		t.Fatalf("remote_dir = %#v", got)
	}
	if got := prepared.Payload["overwrite"]; got != true {
		t.Fatalf("overwrite = %#v", got)
	}
	if prepared.Preview["items"] != 2 {
		t.Fatalf("items preview = %#v", prepared.Preview["items"])
	}
}

func TestPrepareUploadFilesRequiresPathsAndRemoteDir(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionUploadFiles,
		Input:      map[string]any{"local_paths": []string{"/tmp/report.txt"}},
	})
	if err == nil {
		t.Fatal("expected missing remote_dir error")
	}

	_, err = New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: ActionUploadFiles,
		Input:      map[string]any{"remote_dir": "/home/root"},
	})
	if err == nil {
		t.Fatal("expected missing local_paths error")
	}
}

func TestPrepareRejectsUnknownAction(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:worker-2", ConnectorKind: Kind},
		ActionName: "upload_file",
	})
	if !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("expected ErrUnsupportedAction, got %v", err)
	}
}

func TestExecuteActionNotWired(t *testing.T) {
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{}, connectors.PreparedAction{})
	if !errors.Is(err, ErrExecutionNotWired) {
		t.Fatalf("expected ErrExecutionNotWired, got %v", err)
	}
}

func hasField(schema connectors.Schema, name string) bool {
	for _, field := range schema.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}
