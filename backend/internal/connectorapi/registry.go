// Package connectorapi owns the optional gateway adapter registry for
// connector capabilities that cannot be implemented by the structured action
// interface alone.
package connectorapi

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

// Adapter is a marker implemented by connector-owned gateway adapters.
//
// Normal structured connectors do not need an Adapter. Runtime-backed
// connectors can register one from their own connector package.
type Adapter interface{}

// RouteMux is the minimal HTTP router surface connector setup adapters can use.
type RouteMux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// GatewayRuntime exposes only connector-safe runtime resources from the
// unlocked local database. Connector adapters should depend on this interface,
// not the concrete API runtime.
type GatewayRuntime interface {
	ConnectorDatabase() *sql.DB
	ConnectorVault() *vault.Vault
	ConnectorResource(kind string, name string) any
	ConnectorConsoleSessions() *console.Manager
}

// GatewayServer exposes shared gateway services that connector adapters may
// call without importing the generic API package.
type GatewayServer interface {
	ConnectorActiveRuntimeAvailable(w http.ResponseWriter) bool
	ConnectorTrustStorePath() string
	ConnectorRestartConsoleSession(ctx context.Context, runtime GatewayRuntime, runtimeID int64, runningRequestError string) (ConsoleRestartResult, error)
	ConnectorFinishActionRequest(ctx context.Context, runtime GatewayRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error)
	ConnectorCreateDownloadBatch(ctx context.Context, runtime GatewayRuntime, runtimeID int64, remotePaths []string, archiveName string, source string, status string) (filetransfer.BatchRecord, error)
	ConnectorRunTransferBatch(runtime GatewayRuntime, batchID int64, overwrite bool)
}

// TargetLifecycleGateway is passed to target/profile lifecycle adapters.
type TargetLifecycleGateway interface {
	ConnectorServer() GatewayServer
	ConnectorStaleActionRequestsForTarget(ctx context.Context, runtime GatewayRuntime, targetID int64, profileID int64, reason string) (int64, error)
	ConnectorWriteAudit(ctx context.Context, runtime GatewayRuntime, actorType string, tokenID *int64, runtimeID int64, action string, payload any)
	ConnectorFinalizeDeletedTarget(ctx context.Context, runtime GatewayRuntime, target connectortargets.Target, staleReason string, payload map[string]any) (int64, error)
}

// CredentialResourceGateway is passed to connector-owned credential resource
// adapters.
type CredentialResourceGateway interface {
	ConnectorServer() GatewayServer
}

var (
	mu       sync.RWMutex
	adapters = map[string]Adapter{}
)

// Register installs one connector-owned gateway adapter.
//
// Adapter registration is expected to happen at package init time for built-in
// runtime-backed connectors. Duplicate registrations are programmer errors:
// silently replacing an adapter would make connector capabilities depend on
// import order.
func Register(kind string, adapter Adapter) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		panic("connector adapter kind is required")
	}
	if adapter == nil {
		panic(fmt.Sprintf("connector adapter %q is nil", kind))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := adapters[kind]; exists {
		panic(fmt.Sprintf("connector adapter %q already registered", kind))
	}
	adapters[kind] = adapter
}

// For returns the registered adapter for a connector kind.
func For(kind string) Adapter {
	mu.RLock()
	defer mu.RUnlock()
	return adapters[strings.TrimSpace(kind)]
}

// RuntimeAdapter lets a connector provide gateway-owned async/runtime services.
type RuntimeAdapter interface {
	RuntimeCapabilities(server GatewayServer, runtime GatewayRuntime) map[string]connectors.RuntimeCapability
	SupportsRunning(prepared actions.PreparedRequest) bool
	FinishRunning(server GatewayServer, runtime GatewayRuntime, requestID int64, prepared actions.PreparedRequest)
	RunningHint(request connectortargets.ActionRequest) string
}

// RouteRegistrar lets a connector own compatibility/setup routes without
// placing connector-specific handlers in the generic API package.
type RouteRegistrar interface {
	RegisterRoutes(mux RouteMux, server GatewayServer)
}

// RuntimeResourceProvider lets a connector initialize encrypted local resource
// stores for one unlocked database without making the generic runtime own those
// resource types.
type RuntimeResourceProvider interface {
	RuntimeResources(database *sql.DB, vault *vault.Vault) map[string]any
}

// LiveConsoleAdapter marks an adapter with a persistent console action.
type LiveConsoleAdapter interface {
	LiveConsoleActionName() string
}

// DraftTester lets a connector test a not-yet-persisted target/profile draft.
type DraftTester interface {
	TestDraft(handler TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime, request any)
}

// TargetDeleter lets a connector customize deletion behavior.
type TargetDeleter interface {
	DeleteTarget(handler TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime, target connectortargets.Target)
}

// CredentialProfileLifecycleAdapter lets a connector react to profile lifecycle
// changes without putting connector-specific branches in the core handlers.
type CredentialProfileLifecycleAdapter interface {
	BeforeCreateCredentialProfile(ctx context.Context, runtime GatewayRuntime, store *connectortargets.Store, target connectortargets.Target) error
	BeforeDeleteCredentialProfile(ctx context.Context, handler TargetLifecycleGateway, runtime GatewayRuntime, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error
}

// CredentialProfileTester lets a connector test an existing profile.
type CredentialProfileTester interface {
	TestCredentialProfile(handler TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime, target connectors.TargetView, profile connectors.CredentialProfileView)
}

// TargetOperationRunner runs connector-specific target operations.
type TargetOperationRunner interface {
	RunTargetOperation(handler TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime, target connectortargets.Target, operation string)
}

// CredentialCanonicalizer normalizes public credential profile metadata.
type CredentialCanonicalizer interface {
	CanonicalCredentialPublic(ctx context.Context, handler TargetLifecycleGateway, runtime GatewayRuntime, credentialKind string, public map[string]any) (map[string]any, error)
}

// LiveConsoleTargetAdapter exposes metadata for live-console targets.
type LiveConsoleTargetAdapter interface {
	LiveConsoleCapabilityKind() string
	LiveConsoleTargetRef(ctx context.Context, runtime GatewayRuntime, runtimeID int64) (string, error)
	ResolveLiveConsoleMaterial(ctx context.Context, runtime GatewayRuntime, runtimeID int64) (any, any, error)
	LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any
}

// LiveConsoleTransportAdapter opens a connector-owned persistent runtime for
// the generic live console manager.
type LiveConsoleTransportAdapter interface {
	OpenLiveConsole(ctx context.Context, server GatewayServer, runtime GatewayRuntime, runtimeID int64, rows int, cols int) (*console.RuntimeSession, error)
}

// TCPTransportAdapter lets one connector provide a reviewed TCP transport for
// another connector without exposing connector-specific material to core or to
// the caller. The provider connector owns credential resolution and transport
// setup; the caller connector owns the protocol spoken over the returned conn.
type TCPTransportAdapter interface {
	DialConnectorTCP(ctx context.Context, server GatewayServer, runtime GatewayRuntime, targetRef string, network string, address string) (net.Conn, error)
}

type TransferProgress func(transferred int64, total int64)

type TransferOptions struct {
	Progress TransferProgress
	Wait     func(context.Context) error
}

type TransferResult struct {
	Bytes          int64
	Size           int64
	ChecksumSHA256 string
	DurationMS     int64
}

type RemoteFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type RemotePathStatus struct {
	Exists bool   `json:"exists"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
}

type FileTransferAdapter interface {
	BrowseRemoteFiles(ctx context.Context, server GatewayServer, runtime GatewayRuntime, runtimeID int64, remotePath string) ([]RemoteFileEntry, error)
	StatRemotePath(ctx context.Context, server GatewayServer, runtime GatewayRuntime, runtimeID int64, remotePath string) (RemotePathStatus, error)
	UploadFile(ctx context.Context, server GatewayServer, runtime GatewayRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options TransferOptions) (TransferResult, error)
	DownloadFile(ctx context.Context, server GatewayServer, runtime GatewayRuntime, runtimeID int64, remotePath string, localPath string, options TransferOptions) (TransferResult, error)
}

type ErrorPresenter interface {
	WriteConnectorError(w http.ResponseWriter, err error) bool
	ConnectorErrorMessage(prefix string, err error) string
}

// CredentialResourceAdapter manages connector-owned credential resources.
type CredentialResourceAdapter interface {
	ListCredentialResources(handler CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime)
	CreateCredentialResource(handler CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime)
	ImportCredentialResource(handler CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime)
	GetCredentialResource(handler CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime)
	UpdateCredentialResource(handler CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime)
	DeleteCredentialResource(handler CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime GatewayRuntime)
}

// ConsoleRestartResult is the connector-neutral shape returned by live runtime
// adapters when a persistent session is closed and running requests are
// canceled.
type ConsoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}
