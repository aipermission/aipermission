package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestRuntimePrepareConnectorActionUsesSSHConnectorProfile(t *testing.T) {
	database := openAPITestDB(t)
	profile := createTestSSHConnectorProfile(t, database, sshkeys.NewStore(database, openAPITestVault(t), "connector-actions-test-workspace"), "core-1")
	targetRef := profile.TargetRef
	runtime := &databaseRuntime{database: database, registry: testConnectorRegistry(t)}

	prepared, err := runtime.prepareConnectorAction(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  targetRef,
		ActionName: sshconnector.ActionExec,
		Input:      map[string]any{"command": "uptime"},
		Reason:     "smoke",
		CreatedAt:  time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("prepare connector action: %v", err)
	}

	if prepared.Action.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", prepared.Action.ConnectorKind)
	}
	if prepared.Action.TargetRef != targetRef {
		t.Fatalf("target ref = %q", prepared.Action.TargetRef)
	}
	if prepared.Action.ProfileID < 1 {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Payload["command"] != "uptime" {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func TestConnectorRuntimeCapabilitiesAreKindScoped(t *testing.T) {
	catalog := newTestConnectorCatalog(t)
	server := &Server{adapterRegistry: catalog.adapters}
	runtime := &databaseRuntime{adapterRegistry: catalog.adapters}
	capabilities := connectorRuntimeCapabilitiesFor(postgresconnector.Kind, server, runtime)
	if capabilities == nil || capabilities.RuntimeCapability(connectors.NetworkTransportCapabilityName) == nil {
		t.Fatalf("postgres should receive generic network transport capability: %#v", capabilities)
	}
	if capabilities.RuntimeCapability(sshconnector.RuntimeServiceName) != nil {
		t.Fatalf("postgres should not receive ssh live runtime capability: %#v", capabilities)
	}
	capabilities = connectorRuntimeCapabilitiesFor(sshconnector.Kind, server, runtime)
	if capabilities == nil || capabilities.RuntimeCapability(sshconnector.RuntimeServiceName) == nil {
		t.Fatalf("ssh runtime capability missing: %#v", capabilities)
	}
	if capabilities.RuntimeCapability(connectors.NetworkTransportCapabilityName) == nil {
		t.Fatalf("ssh generic network transport capability missing: %#v", capabilities)
	}
}

func TestConnectorNetworkTransportFailsClosedWithoutSourceIdentity(t *testing.T) {
	database := openAPITestDB(t)
	transport := connectorNetworkTransport{runtime: &databaseRuntime{database: database}}

	_, err := transport.DialConnectorTCP(context.Background(), connectors.NetworkDialRequest{
		Mode:               "over_ssh",
		Host:               "127.0.0.1",
		Port:               5432,
		TransportTargetRef: "ssh:1:1",
	})
	if err == nil || !strings.Contains(err.Error(), "source target or project identity is required") {
		t.Fatalf("expected missing source identity error, got %v", err)
	}
}

func TestConnectorTransportRejectsUndeclaredApprovalDependency(t *testing.T) {
	database := openAPITestDB(t)
	runtime := &databaseRuntime{database: database}
	transport := connectorNetworkTransport{
		runtime:  runtime,
		approved: newApprovedConnectorTransports(nil),
	}

	_, err := transport.DialConnectorTCP(t.Context(), connectors.NetworkDialRequest{
		Mode:               "over_ssh",
		Host:               "127.0.0.1",
		Port:               5432,
		SourceTargetRef:    "postgres:1:1",
		TransportTargetRef: "ssh:2:2",
	})
	if !errors.Is(err, errConnectorTransportApprovalChanged) {
		t.Fatalf("undeclared transport dependency error = %v", err)
	}
}

func TestConnectorTransportRejectsDependencyDriftBeforeUse(t *testing.T) {
	database := openAPITestDB(t)
	vault := openAPITestVault(t)
	profile := createTestSSHConnectorProfile(t, database, sshkeys.NewStore(database, vault, "transport-drift-workspace"), "transport")
	store := connectortargets.NewStore(database)
	targetView, profileView, err := store.ResolveConnectorActionTarget(t.Context(), profile.TargetRef)
	if err != nil {
		t.Fatalf("resolve transport dependency: %v", err)
	}
	approved := newApprovedConnectorTransports([]actions.ResolvedDependency{{
		Purpose: connectors.NetworkTransportCapabilityName,
		Target:  targetView,
		Profile: profileView,
	}})

	if _, err := store.UpdateCredentialProfile(t.Context(), connectortargets.UpdateCredentialProfileInput{
		TargetID:      profile.TargetID,
		ProfileID:     profile.ProfileID,
		ConnectorKind: sshconnector.Kind,
		Kind:          "private_key",
		Label:         "changed-after-approval",
		Public:        profileView.Public,
	}); err != nil {
		t.Fatalf("update transport profile: %v", err)
	}

	release, err := approved.acquire(t.Context(), &databaseRuntime{database: database}, connectors.NetworkTransportCapabilityName, profile.TargetRef)
	if !errors.Is(err, errConnectorTransportApprovalChanged) {
		if release != nil {
			release()
		}
		t.Fatalf("changed transport dependency error = %v", err)
	}
}

func TestConnectorApprovalContextHashesConnectorAndActionDefinition(t *testing.T) {
	prepared := actions.PreparedRequest{
		Target: connectors.TargetView{
			ID:            1,
			Ref:           "postgres:1:2",
			ConnectorKind: postgresconnector.Kind,
			Name:          "main-db",
			Config:        map[string]any{"host": "127.0.0.1", "database": "app"},
		},
		Profile: connectors.CredentialProfileView{
			ID:             2,
			TargetID:       1,
			Kind:           "username_password",
			Label:          "readonly",
			Public:         map[string]any{"username": "app_readonly"},
			RiskLabel:      "read-only",
			UpdatedAt:      "2026-06-12T11:59:00Z",
			SecretRevision: "secret-revision-a",
		},
		ConnectorVersion: "0.1",
		ActionDefinition: connectors.ActionDefinition{
			Name:        postgresconnector.ActionQueryReadonly,
			Label:       "Query read-only",
			Description: "Run bounded read-only SQL.",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "sql", Type: connectors.FieldString, Required: true},
			}},
		},
		Action: connectors.PreparedAction{
			ConnectorKind: postgresconnector.Kind,
			TargetRef:     "postgres:1:2",
			ActionName:    postgresconnector.ActionQueryReadonly,
			Risk:          connectors.RiskRead,
			Payload:       map[string]any{"sql": "select 1"},
		},
		Requested: actions.PrepareRequest{
			Source:     commandRequestSourceMCP,
			TargetRef:  "postgres:1:2",
			ActionName: postgresconnector.ActionQueryReadonly,
			Input:      map[string]any{"sql": "select 1"},
		},
	}
	token := tokens.Token{ID: 3, Name: "codex"}
	permission := connectortargets.ActionPermission{
		TokenID:       token.ID,
		TargetID:      prepared.Target.ID,
		ProfileID:     prepared.Profile.ID,
		ActionName:    prepared.Action.ActionName,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}

	_, baseHash, err := connectorApprovalContext(prepared, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context: %v", err)
	}
	versionChanged := prepared
	identityChanged := prepared
	identityChanged.ActionDefinition.InputSchema.Fields = append([]connectors.Field{}, prepared.ActionDefinition.InputSchema.Fields...)
	identityChanged.ActionDefinition.InputSchema.Fields[0].PreserveWhitespace = true
	_, identityHash, err := connectorApprovalContext(identityChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil || identityHash == baseHash {
		t.Fatalf("opaque field semantics must invalidate approval: %v", err)
	}
	versionChanged.ConnectorVersion = "0.2"
	_, versionHash, err := connectorApprovalContext(versionChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with version change: %v", err)
	}
	if versionHash == baseHash {
		t.Fatalf("connector version drift should change approval hash")
	}
	actionChanged := prepared
	actionChanged.ActionDefinition.Description = "Run read-only SQL with a different contract."
	_, actionHash, err := connectorApprovalContext(actionChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with action definition change: %v", err)
	}
	if actionHash == baseHash {
		t.Fatalf("action definition drift should change approval hash")
	}
	retryChanged := prepared
	retryChanged.ActionDefinition.RetryPolicy = connectors.RetryPolicy{Class: connectors.RetryIdempotent}
	_, retryHash, err := connectorApprovalContext(retryChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with retry policy change: %v", err)
	}
	if retryHash == baseHash {
		t.Fatalf("retry policy drift should change approval hash")
	}
	sensitiveFieldsChanged := prepared
	sensitiveFieldsChanged.ActionDefinition.SensitiveInputFields = []string{"sql"}
	_, sensitiveFieldsHash, err := connectorApprovalContext(sensitiveFieldsChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with sensitive input field change: %v", err)
	}
	if sensitiveFieldsHash == baseHash {
		t.Fatalf("sensitive input field drift should change approval hash")
	}
	profileChanged := prepared
	profileChanged.Profile.SecretRevision = "secret-revision-b"
	_, profileHash, err := connectorApprovalContext(profileChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with profile revision change: %v", err)
	}
	if profileHash == baseHash {
		t.Fatalf("credential profile revision drift should change approval hash")
	}
	withDependency := prepared
	withDependency.Dependencies = []actions.ResolvedDependency{{
		Purpose: "network_transport",
		Target: connectors.TargetView{
			ID: 7, ProjectID: 4, Ref: "ssh:7:11", ConnectorKind: "ssh", Name: "gateway", UpdatedAt: "2026-06-12T11:58:00Z",
		},
		Profile: connectors.CredentialProfileView{
			ID: 11, TargetID: 7, ConnectorKind: "ssh", Kind: "private_key", Label: "root", UpdatedAt: "2026-06-12T11:58:00Z", SecretRevision: "ssh-secret-a",
		},
	}}
	dependencyContext, dependencyHash, err := connectorApprovalContext(withDependency, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with dependency: %v", err)
	}
	dependencyChanged := withDependency
	dependencyChanged.Dependencies = append([]actions.ResolvedDependency(nil), withDependency.Dependencies...)
	dependencyChanged.Dependencies[0].Profile.SecretRevision = "ssh-secret-b"
	dependencyChangedContext, dependencyChangedHash, err := connectorApprovalContext(dependencyChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with dependency revision change: %v", err)
	}
	if dependencyChangedHash == dependencyHash {
		t.Fatalf("approval dependency revision drift should change approval hash")
	}
	if drift := connectorApprovalDriftReason(dependencyContext, dependencyChangedContext); drift != "dependencies" {
		t.Fatalf("approval dependency drift reason = %q", drift)
	}
	tokenRenamed := token
	tokenRenamed.Name = "renamed-token-label"
	_, renamedTokenHash, err := connectorApprovalContext(prepared, tokenRenamed, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with token rename: %v", err)
	}
	if renamedTokenHash != baseHash {
		t.Fatalf("token label changes should not change approval hash")
	}
}
