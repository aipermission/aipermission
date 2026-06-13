package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type connectorRuntimeAdapter interface {
	RuntimeServices(server *Server, runtime *databaseRuntime) map[string]any
	SupportsRunning(prepared actions.PreparedRequest) bool
	FinishRunning(server *Server, runtime *databaseRuntime, requestID int64, prepared actions.PreparedRequest)
	RunningHint(request connectortargets.ActionRequest) string
}

type connectorLiveConsoleAdapter interface {
	LiveConsoleActionName() string
}

type connectorDraftTester interface {
	TestDraft(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, request createConnectorTargetRequest)
}

type connectorTargetDeleter interface {
	DeleteTarget(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectortargets.Target)
}

type connectorCredentialProfileLifecycleAdapter interface {
	BeforeCreateCredentialProfile(ctx context.Context, runtime *databaseRuntime, store *connectortargets.Store, target connectortargets.Target) error
	BeforeDeleteCredentialProfile(ctx context.Context, handler connectorTargetHandlers, runtime *databaseRuntime, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error
}

type connectorCredentialProfileTester interface {
	TestCredentialProfile(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectors.TargetView, profile connectors.CredentialProfileView)
}

type connectorTargetOperationRunner interface {
	RunTargetOperation(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectortargets.Target, operation string)
}

type connectorCredentialCanonicalizer interface {
	CanonicalCredentialPublic(ctx context.Context, handler connectorTargetHandlers, runtime *databaseRuntime, credentialKind string, public map[string]any) (map[string]any, error)
}

type connectorLiveConsoleTargetAdapter interface {
	LiveConsoleProfileID(profileID int64) int64
	LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any
}

type connectorCredentialResourceAdapter interface {
	ListCredentialResources(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime)
	CreateCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime)
	ImportCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime)
	GetCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime)
	UpdateCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime)
	DeleteCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime)
}

type connectorAPIAdapter interface{}

var connectorAPIAdapters = map[string]connectorAPIAdapter{
	sshconnector.Kind: sshRuntimeAdapter{},
}

func connectorAPIAdapterFor(kind string) connectorAPIAdapter {
	return connectorAPIAdapters[strings.TrimSpace(kind)]
}

func connectorRuntimeAdapterFor(kind string) connectorRuntimeAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorRuntimeAdapter)
	return adapter
}

func connectorDraftTesterFor(kind string) connectorDraftTester {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorDraftTester)
	return adapter
}

func connectorTargetDeleterFor(kind string) connectorTargetDeleter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorTargetDeleter)
	return adapter
}

func connectorCredentialProfileLifecycleAdapterFor(kind string) connectorCredentialProfileLifecycleAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorCredentialProfileLifecycleAdapter)
	return adapter
}

func connectorCredentialProfileTesterFor(kind string) connectorCredentialProfileTester {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorCredentialProfileTester)
	return adapter
}

func connectorTargetOperationRunnerFor(kind string) connectorTargetOperationRunner {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorTargetOperationRunner)
	return adapter
}

func connectorCredentialCanonicalizerFor(kind string) connectorCredentialCanonicalizer {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorCredentialCanonicalizer)
	return adapter
}

func connectorLiveConsoleTargetAdapterFor(kind string) connectorLiveConsoleTargetAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorLiveConsoleTargetAdapter)
	return adapter
}
