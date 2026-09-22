package gatewayconnectormanagement

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type credentialResourceLockAdapter struct {
	t      *testing.T
	locked *bool
	calls  []string
}

func (adapter *credentialResourceLockAdapter) record(w http.ResponseWriter, operation string, wantLocked bool) {
	adapter.t.Helper()
	if *adapter.locked != wantLocked {
		adapter.t.Fatalf("%s locked = %t, want %t", operation, *adapter.locked, wantLocked)
	}
	adapter.calls = append(adapter.calls, operation)
	w.WriteHeader(http.StatusNoContent)
}

func (adapter *credentialResourceLockAdapter) ListCredentialResources(w http.ResponseWriter, _ *http.Request, _ connectorapi.CredentialResourceRuntime) {
	adapter.record(w, "list", false)
}
func (adapter *credentialResourceLockAdapter) CreateCredentialResource(w http.ResponseWriter, _ *http.Request, _ connectorapi.CredentialResourceRuntime) {
	adapter.record(w, "create", true)
}
func (adapter *credentialResourceLockAdapter) ImportCredentialResource(w http.ResponseWriter, _ *http.Request, _ connectorapi.CredentialResourceRuntime) {
	adapter.record(w, "import", true)
}
func (adapter *credentialResourceLockAdapter) GetCredentialResource(w http.ResponseWriter, _ *http.Request, _ connectorapi.CredentialResourceRuntime) {
	adapter.record(w, "get", false)
}
func (adapter *credentialResourceLockAdapter) UpdateCredentialResource(w http.ResponseWriter, _ *http.Request, _ connectorapi.CredentialResourceRuntime) {
	adapter.record(w, "update", true)
}
func (adapter *credentialResourceLockAdapter) DeleteCredentialResource(w http.ResponseWriter, _ *http.Request, _ connectorapi.CredentialResourceRuntime) {
	adapter.record(w, "delete", true)
}

func TestCredentialResourceMutationsHoldWorkspaceLifecycleGate(t *testing.T) {
	locked := false
	acquired := 0
	released := 0
	adapter := &credentialResourceLockAdapter{t: t, locked: &locked}
	component := New(Dependencies{Active: func(http.ResponseWriter) (Workspace, bool) {
		return Workspace{
			Storage: StoragePorts{AcquireExclusive: func(_ context.Context) (func(), error) {
				if locked {
					t.Fatal("credential resource lifecycle gate acquired recursively")
				}
				locked = true
				acquired++
				return func() {
					locked = false
					released++
				}, nil
			}},
			Credentials: CredentialPorts{ResourceRuntime: func(string) connectorapi.CredentialResourceRuntime { return targetDraftRuntime{} }},
		}, true
	}})
	handlers := component.CredentialResources(CredentialResourceDependencies{
		Adapter: func(string) CredentialResourceAdapter { return adapter },
		WriteError: func(w http.ResponseWriter, status int, message string) {
			http.Error(w, message, status)
		},
	})

	operations := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "list", handler: handlers.List},
		{name: "create", handler: handlers.Create},
		{name: "import", handler: handlers.Import},
		{name: "get", handler: handlers.Get},
		{name: "update", handler: handlers.Update},
		{name: "delete", handler: handlers.Delete},
	}
	for _, operation := range operations {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.SetPathValue("kind", "ssh")
		response := httptest.NewRecorder()
		operation.handler(response, request)
		if response.Code != http.StatusNoContent || locked {
			t.Fatalf("%s response=%d locked=%t", operation.name, response.Code, locked)
		}
	}
	if acquired != 4 || released != 4 {
		t.Fatalf("gate acquired=%d released=%d", acquired, released)
	}
}

func TestCredentialResourceMutationReleasesPartialAcquisition(t *testing.T) {
	released := 0
	component := New(Dependencies{Active: func(http.ResponseWriter) (Workspace, bool) {
		return Workspace{
			Storage: StoragePorts{AcquireExclusive: func(context.Context) (func(), error) {
				return func() { released++ }, errors.New("acquisition canceled")
			}},
			Credentials: CredentialPorts{ResourceRuntime: func(string) connectorapi.CredentialResourceRuntime { return targetDraftRuntime{} }},
		}, true
	}})
	handlers := component.CredentialResources(CredentialResourceDependencies{
		Adapter:    func(string) CredentialResourceAdapter { return &credentialResourceLockAdapter{t: t, locked: new(bool)} },
		WriteError: func(w http.ResponseWriter, status int, message string) { http.Error(w, message, status) },
	})
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.SetPathValue("kind", "ssh")
	response := httptest.NewRecorder()
	handlers.Create(response, request)
	if released != 1 || response.Code != http.StatusRequestTimeout {
		t.Fatalf("released=%d status=%d", released, response.Code)
	}
}
