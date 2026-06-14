// Package connectorapi owns the optional gateway adapter registry for
// connector capabilities that cannot be implemented by the structured action
// interface alone.
package connectorapi

import (
	"context"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
)

// Adapter is a marker implemented by connector-owned gateway adapters.
//
// Normal structured connectors do not need an Adapter. Runtime-backed
// connectors can register one from their own connector package.
type Adapter interface{}

var (
	mu       sync.RWMutex
	adapters = map[string]Adapter{}
)

// Register installs one connector-owned gateway adapter.
func Register(kind string, adapter Adapter) {
	kind = strings.TrimSpace(kind)
	if kind == "" || adapter == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
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
	RuntimeServices(server any, runtime any) map[string]any
	SupportsRunning(prepared actions.PreparedRequest) bool
	FinishRunning(server any, runtime any, requestID int64, prepared actions.PreparedRequest)
	RunningHint(request connectortargets.ActionRequest) string
}

// RouteRegistrar lets a connector own compatibility/setup routes without
// placing connector-specific handlers in the generic API package.
type RouteRegistrar interface {
	RegisterRoutes(mux any, server any)
}

// RuntimeResourceProvider lets a connector initialize encrypted local resource
// stores for one unlocked database without making the generic runtime own those
// resource types.
type RuntimeResourceProvider interface {
	RuntimeResources(database any, vault any) map[string]any
}

// LiveConsoleAdapter marks an adapter with a persistent console action.
type LiveConsoleAdapter interface {
	LiveConsoleActionName() string
}

// DraftTester lets a connector test a not-yet-persisted target/profile draft.
type DraftTester interface {
	TestDraft(handler any, w any, r any, runtime any, request any)
}

// TargetDeleter lets a connector customize deletion behavior.
type TargetDeleter interface {
	DeleteTarget(handler any, w any, r any, runtime any, target connectortargets.Target)
}

// CredentialProfileLifecycleAdapter lets a connector react to profile lifecycle
// changes without putting connector-specific branches in the core handlers.
type CredentialProfileLifecycleAdapter interface {
	BeforeCreateCredentialProfile(ctx any, runtime any, store *connectortargets.Store, target connectortargets.Target) error
	BeforeDeleteCredentialProfile(ctx any, handler any, runtime any, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error
}

// CredentialProfileTester lets a connector test an existing profile.
type CredentialProfileTester interface {
	TestCredentialProfile(handler any, w any, r any, runtime any, target connectors.TargetView, profile connectors.CredentialProfileView)
}

// TargetOperationRunner runs connector-specific target operations.
type TargetOperationRunner interface {
	RunTargetOperation(handler any, w any, r any, runtime any, target connectortargets.Target, operation string)
}

// CredentialCanonicalizer normalizes public credential profile metadata.
type CredentialCanonicalizer interface {
	CanonicalCredentialPublic(ctx any, handler any, runtime any, credentialKind string, public map[string]any) (map[string]any, error)
}

// LiveConsoleTargetAdapter exposes metadata for live-console targets.
type LiveConsoleTargetAdapter interface {
	LiveConsoleRuntimeID(target connectors.TargetView, profile connectors.CredentialProfileView) int64
	LiveConsoleTargetRef(ctx any, runtime any, runtimeID int64) (string, error)
	ResolveLiveConsoleMaterial(ctx any, runtime any, runtimeID int64) (any, any, error)
	LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any
}

// LiveConsoleTransportAdapter opens a connector-owned persistent runtime for
// the generic live console manager.
type LiveConsoleTransportAdapter interface {
	OpenLiveConsole(ctx context.Context, server any, runtime any, runtimeID int64, rows int, cols int) (*console.RuntimeSession, error)
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
	BrowseRemoteFiles(ctx context.Context, server any, runtime any, runtimeID int64, remotePath string) ([]RemoteFileEntry, error)
	StatRemotePath(ctx context.Context, server any, runtime any, runtimeID int64, remotePath string) (RemotePathStatus, error)
	UploadFile(ctx context.Context, server any, runtime any, runtimeID int64, localPath string, remotePath string, overwrite bool, options TransferOptions) (TransferResult, error)
	DownloadFile(ctx context.Context, server any, runtime any, runtimeID int64, remotePath string, localPath string, options TransferOptions) (TransferResult, error)
}

type ErrorPresenter interface {
	WriteConnectorError(w any, err error) bool
	ConnectorErrorMessage(prefix string, err error) string
}

// CredentialResourceAdapter manages connector-owned credential resources.
type CredentialResourceAdapter interface {
	ListCredentialResources(handler any, w any, r any, runtime any)
	CreateCredentialResource(handler any, w any, r any, runtime any)
	ImportCredentialResource(handler any, w any, r any, runtime any)
	GetCredentialResource(handler any, w any, r any, runtime any)
	UpdateCredentialResource(handler any, w any, r any, runtime any)
	DeleteCredentialResource(handler any, w any, r any, runtime any)
}

// ConsoleRestartResult is the connector-neutral shape returned by live runtime
// adapters when a persistent session is closed and running requests are
// canceled.
type ConsoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}
