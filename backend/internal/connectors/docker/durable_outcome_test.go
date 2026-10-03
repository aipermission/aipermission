package dockerconnector_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

const durableDockerWorkspace = "docker-durable-test"
const durableDockerEOF = "Post /containers/111111111111/restart: EOF"

func TestDockerDurableUnknownOutcomeSameKeyReplay(t *testing.T) {
	for _, action := range []string{dockerconnector.ActionRestartContainer, dockerconnector.ActionContainerExec} {
		t.Run(action, func(t *testing.T) {
			dependencies, transport, call := durableDockerFixture(t, action)
			newRuntime := func() *actions.Runtime {
				runtime, err := actions.NewRuntime(dependencies)
				durableDockerCheck(t, err)
				t.Cleanup(func() { durableDockerCheck(t, runtime.StopFinalizers(context.Background())) })
				return runtime
			}
			first, err := newRuntime().Call(t.Context(), call)
			durableDockerCheck(t, err)
			if first.Replayed || first.Request.ID < 1 || first.Result.Status != connectors.ResultOutcomeUnknown || transport.mutations != 1 {
				t.Fatalf("first call = %#v; mutations=%d", first, transport.mutations)
			}
			stored, err := connectortargets.NewStore(dependencies.Database).GetActionRequest(t.Context(), first.Request.ID)
			durableDockerCheck(t, err)
			if stored.Status != connectors.ResultOutcomeUnknown || stored.CompletedAt == nil || stored.DisplayText != "" || stored.Error == "" {
				t.Fatalf("durable request = %#v", stored)
			}
			output, ok := stored.Output.(map[string]any)
			if !ok || output["code"] != "outcome_unknown" || output["dispatch_stage"] != "docker_cli_response" || output["retry_safe"] != false || output["output_withheld"] != true {
				t.Fatalf("durable uncertainty details = %#v", stored.Output)
			}
			encoded, err := json.Marshal(first.Result)
			durableDockerCheck(t, err)
			if first.Result.DisplayText != "" || strings.Contains(string(encoded), durableDockerEOF) || !reflect.DeepEqual(first.Result.Output, stored.Output) {
				t.Fatalf("uncertain response exposed output or differs from storage: %s", encoded)
			}
			envelope, err := dependencies.SealedRecords.OpenActionRequest(stored.ID, stored.EncryptedPayloadJSON)
			durableDockerCheck(t, err)
			if !vault.IsRecordEnvelope(stored.EncryptedPayloadJSON) || envelope.Payload["container"] != "api" || envelope.Payload["command"] != call.Input["command"] || envelope.Reason != call.Reason {
				t.Fatalf("action payload was not record-bound and recoverable: %#v", envelope)
			}
			var historyStatus, historyText, historyOutput, historyError string
			durableDockerCheck(t, dependencies.Database.QueryRowContext(t.Context(), `
				SELECT status, output_text, output_json, error FROM history_entries
				WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, stored.ID).
				Scan(&historyStatus, &historyText, &historyOutput, &historyError))
			expectedHistory, err := json.Marshal(stored.Output)
			durableDockerCheck(t, err)
			if historyStatus != string(stored.Status) || historyText != "" || historyError != stored.Error || historyOutput != string(expectedHistory) || strings.Contains(historyOutput, durableDockerEOF) {
				t.Fatalf("history differs from durable unknown: %s %q %s %q", historyStatus, historyText, historyOutput, historyError)
			}
			var auditPayload string
			durableDockerCheck(t, dependencies.Database.QueryRowContext(t.Context(), `
				SELECT payload_json FROM audit_outbox WHERE action_request_id = ?
				AND action = 'connector_action.request.outcome_unknown'`, stored.ID).Scan(&auditPayload))
			var auditDetails map[string]any
			durableDockerCheck(t, json.Unmarshal([]byte(auditPayload), &auditDetails))
			if auditDetails["status"] != "outcome_unknown" || auditDetails["connector_kind"] != "docker" || auditDetails["action_name"] != action || strings.Contains(auditPayload, durableDockerEOF) {
				t.Fatalf("terminal audit = %s", auditPayload)
			}
			// A fresh runtime must replay the database record without another transport call.
			callsBefore := transport.calls
			replayRuntime := newRuntime()
			replayed, err := replayRuntime.Call(t.Context(), call)
			durableDockerCheck(t, err)
			if !replayed.Replayed || replayed.Request.ID != stored.ID || replayed.Result.Status != stored.Status || replayed.Result.Error != stored.Error || replayed.Result.DisplayText != "" || !reflect.DeepEqual(replayed.Result.Output, stored.Output) || transport.calls != callsBefore || transport.mutations != 1 {
				t.Fatalf("same-key replay = %#v; calls=%d mutations=%d", replayed, transport.calls, transport.mutations)
			}
			var requests, histories, terminalAudits int
			durableDockerCheck(t, dependencies.Database.QueryRowContext(t.Context(), `SELECT
				(SELECT COUNT(*) FROM connector_action_requests WHERE idempotency_key = ?),
				(SELECT COUNT(*) FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?),
				(SELECT COUNT(*) FROM audit_outbox WHERE action_request_id = ? AND action = 'connector_action.request.outcome_unknown')`,
				call.IdempotencyKey, stored.ID, stored.ID).Scan(&requests, &histories, &terminalAudits))
			if requests != 1 || histories != 1 || terminalAudits != 1 {
				t.Fatalf("replay duplicated durable evidence: requests=%d history=%d audit=%d", requests, histories, terminalAudits)
			}
			conflicting := call
			conflicting.Input = map[string]any{"container": "other"}
			_, err = replayRuntime.Call(t.Context(), conflicting)
			if !errors.Is(err, connectortargets.ErrActionRequestIdempotency) || transport.calls != callsBefore {
				t.Fatalf("same-key identity conflict = %v; calls=%d", err, transport.calls)
			}
		})
	}
}

func durableDockerFixture(t *testing.T, action string) (actions.RuntimeDependencies, *durableDockerTransport, actions.Call) {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "docker.aipdb"), "durable-docker-test-password")
	durableDockerCheck(t, err)
	t.Cleanup(func() { durableDockerCheck(t, database.Close()) })
	secretVault, err := vault.New("durable-docker-test-secret")
	durableDockerCheck(t, err)
	records := durableDockerRecords{secretVault}
	store := connectortargets.NewStore(database)
	sshTarget, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: "ssh", Name: "host", Config: map[string]any{}})
	durableDockerCheck(t, err)
	sshProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: sshTarget.ID, ConnectorKind: "ssh", Kind: "private_key", Label: "host"})
	durableDockerCheck(t, err)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "docker", Name: "Docker", Config: map[string]any{"connection_mode": "over_ssh", "docker_command": "docker", "transport_target_ref": connectors.FormatTargetRef("ssh", sshTarget.ID, sshProfile.ID)},
	})
	durableDockerCheck(t, err)
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "docker", Kind: "container_scope", Label: "api", Public: map[string]any{"scope_mode": "selected", "allowed_containers": "api"},
	})
	durableDockerCheck(t, err)
	sealed, err := recordcrypto.EncryptJSON(secretVault, durableDockerWorkspace, recordcrypto.ConnectorCredentialProfile, profile.ID, map[string]any{})
	durableDockerCheck(t, err)
	_, err = store.UpdateCredentialProfile(t.Context(), connectortargets.UpdateCredentialProfileInput{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "docker", Kind: profile.Kind, Label: profile.Label, Public: profile.Public, EncryptedSecretJSON: &sealed,
		ExpectedSecretRevision: &profile.SecretRevision,
	})
	durableDockerCheck(t, err)
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "Docker caller"})
	durableDockerCheck(t, err)
	durableDockerCheck(t, store.SetActionPermission(t.Context(), connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID, ActionName: action, ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}))
	registry := connectors.NewRegistry()
	durableDockerCheck(t, registry.Register(dockerconnector.New()))
	redactor, err := actionresult.NewRedactor(func(_ context.Context, value string) string { return value }, func(_ context.Context, value string) string { return value }, 256<<10)
	durableDockerCheck(t, err)
	transport := &durableDockerTransport{source: connectors.FormatTargetRef("docker", target.ID, profile.ID), peer: connectors.FormatTargetRef("ssh", sshTarget.ID, sshProfile.ID)}
	dependencies := actions.RuntimeDependencies{
		Database: database, Tokens: durableDockerAdmission{token.ID}, Registry: registry, Targets: durableDockerTargets{store},
		IdentityTag: func(value []byte) (string, error) {
			return actions.IdentityTag([]byte("0123456789abcdef0123456789abcdef"), value)
		},
		Delivery: durableDockerAdmission{}, MCPStarted: func() bool { return true },
		Identity: func() (string, string, error) { return durableDockerWorkspace, "docker-runtime", nil },
		Redactor: redactor, SealedRecords: records,
		Mutations:    durableDockerMutations{observability.NewCoordinator(database, nil, func(value string) string { return value }, nil), t},
		Capabilities: func(string, []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver { return transport }, RunningActions: durableDockerAdmission{},
	}
	input := map[string]any{"container": "api"}
	if action == dockerconnector.ActionContainerExec {
		input["command"] = "printf hi"
	}
	return dependencies, transport, actions.Call{TokenID: token.ID, Source: actions.SourceMCP, TargetRef: transport.source, ActionName: action, Input: input, Reason: "Observe daemon reply loss", IdempotencyKey: "docker-unknown-" + action}
}

func durableDockerCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type durableDockerTargets struct{ store *connectortargets.Store }

func (r durableDockerTargets) ResolveActionTarget(ctx context.Context, ref string) (actions.ResolvedTarget, error) {
	target, profile, err := r.store.ResolveConnectorActionTarget(ctx, ref)
	return actions.ResolvedTarget{Target: target, Profile: profile}, err
}

type durableDockerRecords struct{ vault *vault.Vault }

func (r durableDockerRecords) SealActionRequest(id int64, envelope actions.ExecutionEnvelope) (string, error) {
	return recordcrypto.EncryptJSON(r.vault, durableDockerWorkspace, recordcrypto.ConnectorActionRequest, id, envelope)
}

func (r durableDockerRecords) OpenActionRequest(id int64, sealed string) (actions.ExecutionEnvelope, error) {
	var envelope actions.ExecutionEnvelope
	err := recordcrypto.DecryptJSON(r.vault, durableDockerWorkspace, recordcrypto.ConnectorActionRequest, id, sealed, &envelope)
	return envelope, err
}

func (r durableDockerRecords) OpenCredentialProfile(id int64, sealed string) (map[string]any, error) {
	var secrets map[string]any
	err := recordcrypto.DecryptJSON(r.vault, durableDockerWorkspace, recordcrypto.ConnectorCredentialProfile, id, sealed, &secrets)
	return secrets, err
}

type durableDockerMutations struct {
	*observability.Coordinator
	t *testing.T
}

func (m durableDockerMutations) WithTransaction(ctx context.Context, mutate func(*sql.Tx, actions.AuditAppender) error) error {
	return m.Coordinator.WithTransaction(ctx, func(tx *sql.Tx, appendAudit observability.Appender) error {
		return mutate(tx, actions.AuditAppender(appendAudit))
	})
}

func (m durableDockerMutations) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	durableDockerCheck(m.t, m.WriteRequired(ctx, actor, tokenID, runtimeID, action, payload))
}

type durableDockerAdmission struct{ tokenID int64 }

func (a durableDockerAdmission) Get(context.Context, int64, time.Time) (actions.AuthorizationToken, error) {
	return actions.AuthorizationToken{ID: a.tokenID, Active: true}, nil
}
func (durableDockerAdmission) Acquire(context.Context) (func(), error)      { return func() {}, nil }
func (durableDockerAdmission) SupportsRunning(actions.PreparedRequest) bool { return false }
func (durableDockerAdmission) FinishRunning(context.Context, int64, actions.PreparedRequest, executionprincipal.Principal, connectors.ActionHandles) {
	panic("Docker fixture must not launch asynchronous work")
}

type durableDockerTransport struct {
	source, peer     string
	calls, mutations int
}

func (*durableDockerTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}
func (r *durableDockerTransport) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.CommandTransportCapabilityName {
		return r
	}
	return nil
}
func (r *durableDockerTransport) RunConnectorCommand(_ context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	r.calls++
	if request.SourceTargetRef != r.source || request.TransportTargetRef != r.peer || request.Mode != "over_ssh" || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 60 {
		return connectors.CommandRunResult{}, fmt.Errorf("invalid Docker transport request: %#v", request)
	}
	switch request.Command {
	case "command docker version --format '{{json .}}'":
		return connectors.CommandRunResult{DispatchStarted: true, Stdout: `{"Client":{"Version":"28.0.0"},"Server":{"Version":"28.0.0"}}`}, nil
	case "command docker ps -a --no-trunc --format '{{json .}}'":
		return connectors.CommandRunResult{DispatchStarted: true, Stdout: `{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour"}`}, nil
	}
	if strings.HasPrefix(request.Command, "command docker restart --time 10 -- '111111111111' 2>&1") || strings.HasPrefix(request.Command, "command docker exec -- '111111111111' sh -c ") {
		r.mutations++
		return connectors.CommandRunResult{DispatchStarted: true, ExitCode: 1, Stdout: durableDockerEOF}, nil
	}
	return connectors.CommandRunResult{}, fmt.Errorf("unexpected Docker command: %s", request.Command)
}
