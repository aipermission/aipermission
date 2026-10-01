package gatewayconnectormanagement

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type credentialOperationFixtureConnector struct{ targetDraftTestConnector }

func (credentialOperationFixtureConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{Kind: "password", Label: "Password"}}
}

type credentialOnlyOperationAdapter struct {
	run func(context.Context, connectorapi.TargetOperationGateway, connectors.RuntimeContext, string, map[string]any) (connectors.ManagementResponse, error)
}

func (*credentialOnlyOperationAdapter) SupportsCredentialTargetOperation(operation string) bool {
	return operation == "inspect"
}

func (adapter *credentialOnlyOperationAdapter) RunCredentialTargetOperation(ctx context.Context, gateway connectorapi.TargetOperationGateway, runtime connectors.RuntimeContext, operation string, input map[string]any) (connectors.ManagementResponse, error) {
	return adapter.run(ctx, gateway, runtime, operation, input)
}

type credentialFalsePolicyAdapter struct {
	*credentialOnlyOperationAdapter
	t *testing.T
}

func (*credentialFalsePolicyAdapter) RequiresTargetOperationExclusion(string) bool { return false }

func (adapter *credentialFalsePolicyAdapter) RunTargetOperation(context.Context, connectorapi.TargetOperationGateway, connectorapi.ConnectorDataRuntime, connectorapi.Target, string, any) (connectors.ManagementResponse, error) {
	adapter.t.Fatal("credential operation dispatched to ordinary target runner")
	return connectors.ManagementResponse{}, nil
}

type credentialOperationHTTPFixture struct {
	component *Component
	workspace *Workspace
	target    connectortargets.Target
	profile   connectortargets.CredentialProfile
}

// OpenEncrypted requires the native SQLCipher driver; no fallback or skip is used.
func newCredentialOperationHTTPFixture(t *testing.T, adapter connectorapi.Adapter) credentialOperationHTTPFixture {
	t.Helper()
	privateDB := filepath.Join(t.TempDir(), "private-db")
	if err := os.Mkdir(privateDB, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := dbpkg.OpenEncrypted(filepath.Join(privateDB, "credential-operation.aipdb"), "CredentialOperationPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	registry := connectors.NewRegistry()
	if err := registry.Register(credentialOperationFixtureConnector{}); err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	target := createTargetDeleteFixture(t, database)
	for _, label := range []string{"decoy", "selected"} {
		profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
			TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "password", Label: label,
			Public: map[string]any{"user": label}, EncryptedSecretJSON: "encrypted-" + label,
		})
		if err != nil {
			t.Fatal(err)
		}
		if label == "selected" {
			// Keep a nondefault selected profile to catch implicit first-profile lookup.
			adapters := connectorapi.NewRegistry()
			if err := adapters.Register(target.ConnectorKind, adapter); err != nil {
				t.Fatal(err)
			}
			forbidden := credentialOperationTestRuntime(t)
			forbidden.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
				t.Fatal("ordinary credential decryptor used")
				return nil, nil
			}
			forbidden.RuntimeContext = func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, connectormanagement.CredentialBoundary) connectors.RuntimeContext {
				t.Fatal("ordinary credential factory used")
				return connectors.RuntimeContext{}
			}
			forbidden.RedactResult = func(context.Context, connectors.ActionResult, connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
				t.Fatal("ordinary credential projector used")
				return connectors.ActionResult{}, nil
			}
			forbidden.RedactText = func(context.Context, string) string { t.Fatal("ordinary text redactor used"); return "" }
			operationRuntime := credentialOperationTestRuntime(t)
			operationRuntime.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
				t.Fatal("unexpected operation decryption")
				return nil, nil
			}
			workspace := &Workspace{
				Storage: StoragePorts{Database: database, Registry: registry, Admission: &connectors.DeliveryAdmissionIdentity{},
					AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil },
					AcquireDelivery: func(context.Context) (func(), error) {
						t.Fatal("credential operation acquired delivery admission")
						return nil, nil
					},
				},
				Credentials: CredentialPorts{Runtime: CredentialRuntimePorts{value: forbidden}, OperationRuntime: CredentialRuntimePorts{value: operationRuntime}},
				Adapters: TargetAdapterPorts{
					OperationGateway: func(string, int64) connectorapi.TargetOperationGateway { return targetOperationGateway{} },
					DataRuntime: func(string) connectorapi.ConnectorDataRuntime {
						t.Fatal("credential operation constructed ordinary data runtime")
						return nil
					},
				},
			}
			component := New(Dependencies{Active: func(http.ResponseWriter) (Workspace, bool) { return *workspace, true }, Adapters: adapters})
			return credentialOperationHTTPFixture{component, workspace, target, profile}
		}
	}
	t.Fatal("selected fixture profile was not created")
	return credentialOperationHTTPFixture{}
}

func credentialOperationBody(profileID int64) string {
	return `{"profile_id":"` + strconv.FormatInt(profileID, 10) + `","input":{"mode":"inspect"}}`
}

func TestTargetCredentialOperationNativeExclusiveFreshSnapshotAndBoundary(t *testing.T) {
	for _, falsePolicy := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone without data port", true: "dual runner false policy with forbidden data port"}[falsePolicy], func(t *testing.T) {
			adapter := &credentialOnlyOperationAdapter{}
			var capability connectorapi.Adapter = adapter
			if falsePolicy {
				capability = &credentialFalsePolicyAdapter{adapter, t}
			}
			fixture := newCredentialOperationHTTPFixture(t, capability)
			workspace := fixture.workspace
			if !falsePolicy {
				workspace.Adapters.DataRuntime = nil
			}
			held, releases := false, 0
			steps := []string{}
			var response *admittedResponseRecorder
			checkHeld := func(ctx context.Context, step string) {
				if !held || !connectors.DeliveryAdmissionHeld(ctx, workspace.Storage.Admission) {
					t.Fatalf("%s escaped lifecycle admission", step)
				}
				steps = append(steps, step)
			}
			workspace.Storage.AcquireExclusive = func(ctx context.Context) (func(), error) {
				held = true
				steps = append(steps, "acquire")
				if _, err := workspace.Storage.Database.ExecContext(ctx, `UPDATE connector_targets SET name = ? WHERE id = ?`, "fresh-target", fixture.target.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := workspace.Storage.Database.ExecContext(ctx, `UPDATE connector_credential_profiles SET label = ?, public_json = ?, encrypted_secret_json = ? WHERE id = ?`, "fresh-profile", `{"user":"fresh-user"}`, "fresh-encrypted-selected", fixture.profile.ID); err != nil {
					t.Fatal(err)
				}
				return func() {
					if !held || response == nil || response.Body.Len() == 0 {
						t.Fatal("released admission before response completion")
					}
					releases++
					held = false
					steps = append(steps, "release")
				}, nil
			}
			const password, unused, derived, responseSecret = "selected-password", "unread-selected-secret", "derived-adapter-secret", "response-only-secret"
			encoded := base64.StdEncoding.EncodeToString([]byte(password))
			payload := map[string]any{"summary": strings.Join([]string{password, unused, derived, encoded}, " "), "keep": "visible"}
			checkPayload := func(projected any) {
				for _, secret := range []string{password, unused, derived, encoded} {
					if strings.Contains(fmt.Sprint(projected), secret) {
						t.Fatalf("audit leaked %q: %#v", secret, projected)
					}
				}
				if projected.(map[string]any)["keep"] != "visible" {
					t.Fatal("audit lost visible data")
				}
			}
			workspace.Adapters.OperationGateway = func(kind string, targetID int64) connectorapi.TargetOperationGateway {
				if !held || kind != fixture.target.ConnectorKind || targetID != fixture.target.ID {
					t.Fatal("gateway lost admitted target")
				}
				steps = append(steps, "gateway")
				return credentialOperationAuditProbe{
					required: func(ctx context.Context, action string, projected any) error {
						checkHeld(ctx, "required-audit")
						if action != "inspect.required" {
							t.Fatal("required audit action lost")
						}
						checkPayload(projected)
						return nil
					},
					bestEffort: func(ctx context.Context, actor string, token *int64, runtimeID int64, action string, projected any) {
						checkHeld(ctx, "best-effort-audit")
						if actor != "local" || token != nil || runtimeID != 0 || action != "inspect.best" {
							t.Fatal("best effort audit metadata lost")
						}
						checkPayload(projected)
					},
				}
			}
			runtime := &workspace.Credentials.OperationRuntime.value
			runtime.DecryptSecret = func(ctx context.Context, id int64, encrypted string) (map[string]any, error) {
				checkHeld(ctx, "decrypt")
				if id != fixture.profile.ID || encrypted != "fresh-encrypted-selected" {
					t.Fatalf("stale or wrong profile decrypted: %d %q", id, encrypted)
				}
				return map[string]any{"password": password, "unused": unused}, nil
			}
			factory := runtime.RuntimeContext
			runtime.RuntimeContext = func(target connectortargets.Target, profile connectortargets.CredentialProfile, secrets map[string]any, boundary connectormanagement.CredentialBoundary) connectors.RuntimeContext {
				if !held || target.Name != "fresh-target" || profile.Label != "fresh-profile" || profile.Public["user"] != "fresh-user" {
					t.Fatal("runtime factory lost admitted snapshot")
				}
				steps = append(steps, "factory")
				return factory(target, profile, secrets, boundary)
			}
			project := runtime.RedactResult
			runtime.RedactResult = func(ctx context.Context, result connectors.ActionResult, boundary connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
				checkHeld(ctx, "project")
				return project(ctx, result, boundary)
			}
			adapter.run = func(ctx context.Context, gateway connectorapi.TargetOperationGateway, runtime connectors.RuntimeContext, operation string, input map[string]any) (connectors.ManagementResponse, error) {
				checkHeld(ctx, "adapter")
				if operation != "inspect" || !reflect.DeepEqual(input, map[string]any{"mode": "inspect"}) || runtime.Target.Name != "fresh-target" || runtime.Profile.ID != fixture.profile.ID || runtime.Target.Ref != connectors.FormatTargetRef(fixture.target.ConnectorKind, fixture.target.ID, fixture.profile.ID) {
					t.Fatal("adapter lost operation input or fresh selected identity")
				}
				if got, err := runtime.Secrets.GetSecret(ctx, "password"); err != nil || got != password {
					t.Fatalf("secret=%q error=%v", got, err)
				}
				runtime.Secrets.(connectors.SensitiveValueRegistrar).RegisterSensitiveValue(derived)
				if err := gateway.ConnectorWriteTargetAudit(ctx, "inspect.required", payload); err != nil {
					t.Fatal(err)
				}
				gateway.ConnectorWriteAudit(ctx, "local", nil, 0, "inspect.best", payload)
				return connectors.ManagementResponse{StatusCode: http.StatusOK, Payload: map[string]any{"summary": payload["summary"], "extra": responseSecret, "keep": "visible"}, SensitiveValues: []string{responseSecret}}, nil
			}
			response = &admittedResponseRecorder{ResponseRecorder: httptest.NewRecorder(), held: &held}
			fixture.component.HTTPHandlers().TargetOperation.Run(response, targetOperationRequest(fixture.target.ID, " inspect ", credentialOperationBody(fixture.profile.ID)))
			wantSteps := []string{"acquire", "gateway", "decrypt", "factory", "adapter", "project", "required-audit", "project", "best-effort-audit", "project", "release"}
			if response.Code != http.StatusOK || held || releases != 1 || response.unadmittedWrites != 0 || !reflect.DeepEqual(steps, wantSteps) {
				t.Fatalf("response=%d %s held=%t releases=%d unadmitted writes=%d steps=%v", response.Code, response.Body.String(), held, releases, response.unadmittedWrites, steps)
			}
			for _, secret := range []string{password, unused, derived, encoded, responseSecret} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("HTTP response leaked %q", secret)
				}
			}
			if !strings.Contains(response.Body.String(), "visible") {
				t.Fatal("HTTP projection lost visible payload")
			}
		})
	}
}

func TestTargetCredentialOperationNativeRejectsProfilesAndMissingScopedRuntimeBeforeDecrypt(t *testing.T) {
	for _, scenario := range []string{"missing", "inactive", "foreign target", "foreign connector", "unsupported credential kind", "empty credential kind", "missing scoped runtime", "invalid operation input"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := &credentialOnlyOperationAdapter{run: func(context.Context, connectorapi.TargetOperationGateway, connectors.RuntimeContext, string, map[string]any) (connectors.ManagementResponse, error) {
				t.Fatal("invalid credential operation reached adapter")
				return connectors.ManagementResponse{}, nil
			}}
			fixture := newCredentialOperationHTTPFixture(t, adapter)
			workspace := fixture.workspace
			releases, held := 0, false
			workspace.Storage.AcquireExclusive = func(ctx context.Context) (func(), error) {
				held = true
				var query string
				var args []any
				switch scenario {
				case "missing":
					query, args = `DELETE FROM connector_credential_profiles WHERE id = ?`, []any{fixture.profile.ID}
				case "inactive":
					query, args = `UPDATE connector_credential_profiles SET status = 'archived' WHERE id = ?`, []any{fixture.profile.ID}
				case "foreign target":
					foreign, err := connectortargets.NewStore(workspace.Storage.Database).CreateTarget(ctx, connectortargets.CreateTargetInput{ConnectorKind: fixture.target.ConnectorKind, Name: "foreign-target"})
					if err != nil {
						t.Fatal(err)
					}
					query, args = `UPDATE connector_credential_profiles SET target_id = ? WHERE id = ?`, []any{foreign.ID, fixture.profile.ID}
				case "foreign connector":
					query, args = `UPDATE connector_credential_profiles SET connector_kind = 'other' WHERE id = ?`, []any{fixture.profile.ID}
				case "unsupported credential kind":
					query, args = `UPDATE connector_credential_profiles SET kind = 'unsupported' WHERE id = ?`, []any{fixture.profile.ID}
				case "empty credential kind":
					query, args = `UPDATE connector_credential_profiles SET kind = '' WHERE id = ?`, []any{fixture.profile.ID}
				}
				if query != "" {
					if _, err := workspace.Storage.Database.ExecContext(ctx, query, args...); err != nil {
						t.Fatal(err)
					}
				}
				return func() { releases++; held = false }, nil
			}
			if scenario == "missing scoped runtime" {
				workspace.Credentials.OperationRuntime = CredentialRuntimePorts{}
			}
			body := credentialOperationBody(fixture.profile.ID)
			if scenario == "invalid operation input" {
				body = `{"profile_id":1,"input":{}}`
			}
			response := &admittedResponseRecorder{ResponseRecorder: httptest.NewRecorder(), held: &held}
			fixture.component.HTTPHandlers().TargetOperation.Run(response, targetOperationRequest(fixture.target.ID, "inspect", body))
			status := http.StatusNotFound
			if strings.Contains(scenario, "credential kind") || scenario == "invalid operation input" {
				status = http.StatusBadRequest
			}
			if scenario == "missing scoped runtime" {
				status = http.StatusInternalServerError
			}
			if response.Code != status || releases != 1 || held || response.unadmittedWrites != 0 {
				t.Fatalf("response=%d %s releases=%d held=%t unadmitted writes=%d", response.Code, response.Body.String(), releases, held, response.unadmittedWrites)
			}
		})
	}
}

func TestTargetCredentialOperationNativeFailedAdmissionNeverDispatches(t *testing.T) {
	for _, scenario := range []string{"canceled", "missing release", "missing exclusive port", "missing identity", "partial acquisition"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := &credentialOnlyOperationAdapter{run: func(context.Context, connectorapi.TargetOperationGateway, connectors.RuntimeContext, string, map[string]any) (connectors.ManagementResponse, error) {
				t.Fatal("failed admission reached adapter")
				return connectors.ManagementResponse{}, nil
			}}
			fixture := newCredentialOperationHTTPFixture(t, adapter)
			workspace := fixture.workspace
			acquires, releases := 0, 0
			workspace.Adapters.OperationGateway = func(string, int64) connectorapi.TargetOperationGateway {
				t.Fatal("failed admission created gateway")
				return nil
			}
			workspace.Storage.AcquireExclusive = func(context.Context) (func(), error) {
				acquires++
				if scenario == "missing release" {
					return nil, nil
				}
				if scenario == "partial acquisition" {
					return func() { releases++ }, errors.New("private admission failure")
				}
				return nil, context.Canceled
			}
			if scenario == "missing exclusive port" {
				workspace.Storage.AcquireExclusive = nil
			}
			if scenario == "missing identity" {
				workspace.Storage.Admission = nil
			}
			response := httptest.NewRecorder()
			fixture.component.HTTPHandlers().TargetOperation.Run(response, targetOperationRequest(fixture.target.ID, "inspect", credentialOperationBody(fixture.profile.ID)))
			status, wantAcquires, wantReleases := http.StatusRequestTimeout, 1, 0
			if scenario == "missing release" {
				status = http.StatusInternalServerError
			}
			if scenario == "missing exclusive port" || scenario == "missing identity" {
				status, wantAcquires = http.StatusInternalServerError, 0
			}
			if scenario == "partial acquisition" {
				wantReleases = 1
			}
			if response.Code != status || acquires != wantAcquires || releases != wantReleases || strings.Contains(response.Body.String(), "private admission failure") {
				t.Fatalf("response=%d %s acquires=%d releases=%d", response.Code, response.Body.String(), acquires, releases)
			}
		})
	}
}
