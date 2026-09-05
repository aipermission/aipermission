// Package connectorapi owns the optional gateway adapter registry for
// connector capabilities that cannot be implemented by the structured action
// interface alone.
package connectorapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

var (
	ErrRemotePathNotFound           = errors.New("remote path not found")
	ErrTransferLimit                = errors.New("file transfer limit exceeded")
	ErrCredentialResourceNotFound   = errors.New("connector credential resource not found")
	ErrCredentialResourceNameExists = errors.New("connector credential resource name already exists")
)

// Adapter is a marker implemented by connector-owned gateway adapters.
//
// Normal structured connectors do not need an Adapter. Runtime-backed
// connectors can register one from their own connector package.
type Adapter interface{}

// CredentialResource describes one connector-owned encrypted resource without
// exposing its secret payload.
type CredentialResource struct {
	ID           int64
	Name         string
	ResourceType string
	PublicData   string
	Fingerprint  string
	CreatedAt    string
	UpdatedAt    string
}

type CreateCredentialResourceInput struct {
	Name         string
	ResourceType string
	PublicData   string
	Fingerprint  string
	Secret       any
}

type UpdateCredentialResourceInput struct {
	Name       string
	PublicData string
}

// CredentialResourceStore is scoped by core to one connector and resource
// kind. It can never query arbitrary tables or decrypt another resource class.
type CredentialResourceStore interface {
	List(ctx context.Context) ([]CredentialResource, error)
	Get(ctx context.Context, id int64) (CredentialResource, error)
	GetSecret(ctx context.Context, id int64, destination any) error
	Create(ctx context.Context, input CreateCredentialResourceInput) (CredentialResource, error)
	Update(ctx context.Context, id int64, input UpdateCredentialResourceInput) (CredentialResource, error)
	Delete(ctx context.Context, id int64) error
	CountProfileReferences(ctx context.Context, publicField string, numericValue int64) (int, error)
}

// ConnectorDataRuntime exposes only connector target/profile operations. The
// implementation is scoped to the adapter's connector kind by core.
type ConnectorDataRuntime interface {
	ResolveConnectorActionTarget(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error)
	EnsureRuntimeSurface(ctx context.Context, input connectortargets.EnsureRuntimeSurfaceInput) (connectortargets.RuntimeSurface, error)
	ListRuntimeSurfacesForProfile(ctx context.Context, targetID int64, profileID int64, capabilityKind string) ([]connectortargets.RuntimeSurface, error)
	TargetProfileByRuntimeID(ctx context.Context, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error)
	ListCredentialProfiles(ctx context.Context, targetID int64) ([]connectors.CredentialProfileView, error)
	CredentialResources(resourceKind string) CredentialResourceStore
}

// LiveSessionRuntime exposes the generic persistent console manager.
type ConsoleSessionRuntime interface {
	EnsureReady(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) (console.SessionHandle, error)
	Exec(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, command string) (console.ExecResult, error)
	ActiveSnapshot(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) (console.Record, error)
	WaitActive(ctx context.Context, principal executionprincipal.Principal, handle console.SessionHandle) (console.ExecResult, error)
	InterruptActive(ctx context.Context, principal executionprincipal.Principal, handle console.SessionHandle) error
}

type LiveSessionRuntime interface {
	ConnectorDataRuntime
	ConnectorConsoleSessions() ConsoleSessionRuntime
}

// PrincipalRuntime resolves the local human execution principal.
type PrincipalRuntime interface {
	ConnectorLocalExecutionPrincipal() (executionprincipal.Principal, error)
}

// LiveConsoleRuntime contains the persisted target data and connector-owned
// resources needed to open a live transport.
type LiveConsoleRuntime interface {
	ConnectorDataRuntime
}

// ActionRuntime is the bounded runtime surface used by connector-owned runtime
// actions. It intentionally excludes Vault and workspace access.
type ActionRuntime interface {
	LiveSessionRuntime
}

// TransferRuntime is the bounded runtime surface used by connector-owned file
// transfer adapters.
type TransferRuntime interface {
	ConnectorDataRuntime
	ResolveRuntimeContext(ctx context.Context, runtimeID int64, capabilityKind string) (connectors.RuntimeContext, connectortargets.RuntimeSurface, error)
}

// TargetLifecycleRuntime contains only resources needed while testing or
// deleting connector targets and credential profiles.
type TargetLifecycleRuntime interface {
	LiveSessionRuntime
	PrincipalRuntime
}

// CredentialResourceRuntime contains only resources needed by connector-owned
// credential resource screens.
type CredentialResourceRuntime interface {
	ConnectorDataRuntime
}

// RouteDefinition is the canonical runtime and documentation contract for a
// connector-owned HTTP route.
type RouteDefinition struct {
	Method  string
	Path    string
	Handler func(RouteGateway, http.ResponseWriter, *http.Request)
}

// Pattern returns the Go 1.22 ServeMux method/path pattern.
func (r RouteDefinition) Pattern() string {
	return strings.TrimSpace(r.Method) + " " + strings.TrimSpace(r.Path)
}

// RuntimeAvailabilityGateway lets connector-owned setup routes require an
// unlocked local runtime without gaining unrelated gateway authority.
type RuntimeAvailabilityGateway interface {
	ConnectorActiveRuntimeAvailable(w http.ResponseWriter) bool
}

// PeerIdentityGateway exposes the local endpoint identity store without
// granting authority to change it.
type PeerIdentityGateway interface {
	ConnectorTrustStorePath() string
}

// PeerTrustGateway owns endpoint identity changes and their Vault lease
// invalidation boundary.
type PeerTrustGateway interface {
	PeerIdentityGateway
	ConnectorChangeVaultPeerTrust(ctx context.Context, change func() error) error
}

// LiveConsoleGateway opens a nested live transport by target reference without
// exposing the provider connector's runtime data or credential resources.
type LiveConsoleGateway interface {
	PeerIdentityGateway
	ConnectorOpenLiveConsole(ctx context.Context, targetRef string, rows int, cols int, params map[string]any) (*console.RuntimeSession, error)
}

// ConsoleRestartGateway owns cancellation and invalidation for one persistent
// connector console session.
type ConsoleRestartGateway interface {
	ConnectorRestartConsoleSession(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, runningRequestError string) (ConsoleRestartResult, error)
}

// ActionFinishGateway owns completion of an asynchronous connector action.
type ActionFinishGateway interface {
	ConnectorFinishActionRequest(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error)
}

// TransferBatchGateway owns creation and execution of connector download jobs.
type TransferBatchGateway interface {
	ConnectorCreateDownloadBatch(ctx context.Context, runtimeID int64, remotePaths []string, archiveName string, source string, status string) (filetransfer.BatchRecord, error)
	ConnectorRunTransferBatch(batchID int64, overwrite bool)
}

// RuntimeCapabilityGateway exposes connector-owned runtime capabilities to
// adapters executing outside the structured action pipeline.
type RuntimeCapabilityGateway interface {
	ConnectorRuntimeCapabilities() connectors.RuntimeCapabilityResolver
}

type RouteGateway interface {
	RuntimeAvailabilityGateway
	PeerTrustGateway
}

type RuntimeActionGateway interface {
	PeerIdentityGateway
	ConsoleRestartGateway
	TransferBatchGateway
}

type FileTransferGateway interface {
	PeerIdentityGateway
	RuntimeCapabilityGateway
}

// TargetDeletionGateway exposes only the irreversible target-deletion
// boundary and the services needed for connector-owned remote cleanup.
type TargetDeletionGateway interface {
	PeerIdentityGateway
	ConsoleRestartGateway
	ConnectorDeleteTargetRecord(ctx context.Context, target connectortargets.Target, payload map[string]any) error
	ConnectorFinalizeDeletedTarget(ctx context.Context, target connectortargets.Target, staleReason string, payload map[string]any) (int64, error)
}

// TargetOperationGateway exposes observation audit and peer identity to
// connector-owned target operations without granting deletion or session
// lifecycle authority.
type TargetOperationGateway interface {
	PeerIdentityGateway
	ConnectorWriteAudit(ctx context.Context, actorType string, tokenID *int64, runtimeID int64, action string, payload any)
}

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

// Register installs one connector-owned gateway adapter.
//
// Duplicate registrations are rejected so explicit catalog construction cannot
// silently replace capabilities based on registration order.
func (r *Registry) Register(kind string, adapter Adapter) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return errors.New("connector adapter kind is required")
	}
	if adapter == nil {
		return fmt.Errorf("connector adapter %q is nil", kind)
	}
	if r == nil {
		return errors.New("connector adapter registry is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adapters == nil {
		r.adapters = map[string]Adapter{}
	}
	if _, exists := r.adapters[kind]; exists {
		return fmt.Errorf("connector adapter %q already registered", kind)
	}
	r.adapters[kind] = adapter
	return nil
}

// For returns the registered adapter for a connector kind.
func (r *Registry) For(kind string) Adapter {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[strings.TrimSpace(kind)]
}

// Kinds returns a deterministic snapshot of registered connector kinds.
func (r *Registry) Kinds() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	kinds := make([]string, 0, len(r.adapters))
	for kind := range r.adapters {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// RouteDefinitions returns validated connector-owned routes for the requested
// connector kinds. The result is deterministic so runtime registration,
// generated contracts, and tests share one inventory.
func (r *Registry) RouteDefinitions(kinds []string) ([]RouteDefinition, error) {
	routes := []RouteDefinition{}
	seen := map[string]string{}
	for _, rawKind := range kinds {
		kind := strings.TrimSpace(rawKind)
		adapter, _ := r.For(kind).(RouteRegistrar)
		if adapter == nil {
			continue
		}
		for _, route := range adapter.Routes() {
			route.Method = strings.ToUpper(strings.TrimSpace(route.Method))
			route.Path = strings.TrimSpace(route.Path)
			if route.Method == "" {
				return nil, fmt.Errorf("connector adapter %q route method is required", kind)
			}
			if !strings.HasPrefix(route.Path, "/") {
				return nil, fmt.Errorf("connector adapter %q route path %q must start with /", kind, route.Path)
			}
			if route.Handler == nil {
				return nil, fmt.Errorf("connector adapter %q route %s %s has no handler", kind, route.Method, route.Path)
			}
			key := route.Pattern()
			if owner, exists := seen[key]; exists {
				return nil, fmt.Errorf("connector adapters %q and %q both register %s", owner, kind, key)
			}
			seen[key] = kind
			routes = append(routes, route)
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})
	return routes, nil
}

// RuntimeAdapter lets a connector provide gateway-owned async/runtime services.
type RuntimeAdapter interface {
	RuntimeCapabilities(server RuntimeActionGateway, runtime ActionRuntime) map[string]connectors.RuntimeCapability
	SupportsRunning(prepared actions.PreparedRequest) bool
	FinishRunning(server ActionFinishGateway, runtime ActionRuntime, requestID int64, prepared actions.PreparedRequest, principal executionprincipal.Principal, handles connectors.ActionHandles) error
	RunningHint(request connectortargets.ActionRequest) string
}

// RouteRegistrar lets a connector own compatibility/setup routes without
// placing connector-specific handlers in the generic API package. Runtime
// registration and generated REST documentation consume the same definitions.
type RouteRegistrar interface {
	Routes() []RouteDefinition
}

// LiveConsoleAdapter marks an adapter with a persistent console action.
type LiveConsoleAdapter interface {
	LiveConsoleActionName() string
}

// DraftTester lets a connector test a not-yet-persisted target/profile draft.
type DraftTester interface {
	TestDraft(handler PeerIdentityGateway, w http.ResponseWriter, r *http.Request, runtime ConnectorDataRuntime, request any)
}

// TargetDeleter lets a connector customize deletion behavior.
type TargetDeleter interface {
	DeleteTarget(handler TargetDeletionGateway, w http.ResponseWriter, r *http.Request, runtime TargetLifecycleRuntime, target connectortargets.Target)
}

// CredentialProfileLifecycleAdapter lets a connector react to profile lifecycle
// changes without putting connector-specific branches in the core handlers.
type CredentialProfileLifecycleAdapter interface {
	BeforeCreateCredentialProfile(ctx context.Context, runtime TargetLifecycleRuntime, target connectortargets.Target) error
	BeforeDeleteCredentialProfile(ctx context.Context, handler ConsoleRestartGateway, runtime TargetLifecycleRuntime, target connectortargets.Target, profile connectortargets.CredentialProfile) error
}

// CredentialProfileTester lets a connector test an existing profile.
type CredentialProfileTester interface {
	TestCredentialProfile(handler PeerIdentityGateway, w http.ResponseWriter, r *http.Request, runtime ConnectorDataRuntime, target connectors.TargetView, profile connectors.CredentialProfileView)
}

// TargetOperationRunner runs connector-specific target operations.
type TargetOperationRunner interface {
	RunTargetOperation(handler TargetOperationGateway, w http.ResponseWriter, r *http.Request, runtime ConnectorDataRuntime, target connectortargets.Target, operation string)
}

// CredentialCanonicalizer normalizes public credential profile metadata.
type CredentialCanonicalizer interface {
	CanonicalCredentialPublic(ctx context.Context, runtime ConnectorDataRuntime, credentialKind string, public map[string]any) (map[string]any, error)
}

// LiveConsoleTargetAdapter exposes metadata for live-console targets.
type LiveConsoleTargetAdapter interface {
	LiveConsoleCapabilityKind() string
	LiveConsoleTargetRef(ctx context.Context, runtime LiveConsoleRuntime, runtimeID int64) (string, error)
	LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any
}

// LiveConsoleTransportAdapter opens a connector-owned persistent runtime for
// the generic live console manager.
type LiveConsoleTransportAdapter interface {
	OpenLiveConsole(ctx context.Context, server LiveConsoleGateway, runtime LiveConsoleRuntime, request console.RuntimeOpenRequest) (*console.RuntimeSession, error)
}

type LiveConsolePeerIdentityAdapter interface {
	ExpectedLiveConsolePeerIdentities(ctx context.Context, server PeerIdentityGateway, runtime LiveConsoleRuntime, runtimeID int64) ([]string, error)
}

// TCPTransportAdapter lets one connector provide a reviewed TCP transport for
// another connector without exposing connector-specific material to core or to
// the caller. The provider connector owns credential resolution and transport
// setup; the caller connector owns the protocol spoken over the returned conn.
type TCPTransportAdapter interface {
	DialConnectorTCP(ctx context.Context, server PeerIdentityGateway, runtime LiveConsoleRuntime, targetRef string, network string, address string) (net.Conn, error)
}

// CommandTransportAdapter lets one connector run a bounded command template
// through another connector-owned transport without exposing connector-specific
// material to core or to the caller.
type CommandTransportAdapter interface {
	RunConnectorCommand(ctx context.Context, server PeerIdentityGateway, runtime LiveConsoleRuntime, targetRef string, command string) (connectors.CommandRunResult, error)
}

type TransferProgress func(transferred int64, total int64)

type TransferOptions struct {
	Progress TransferProgress
	Wait     func(context.Context) error
	MaxBytes int64
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

type RemoteFilePage struct {
	Entries    []RemoteFileEntry `json:"entries"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more"`
}

type FileTransferAdapter interface {
	BrowseRemoteFiles(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string) ([]RemoteFileEntry, error)
	StatRemotePath(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string) (RemotePathStatus, error)
	UploadFile(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options TransferOptions) (TransferResult, error)
	DownloadFile(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string, localPath string, options TransferOptions) (TransferResult, error)
}

type RecursiveFileTransferAdapter interface {
	ListRecursiveFiles(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string, maxItems int, maxObjectBytes int64, maxBatchBytes int64) ([]RemoteFileEntry, error)
}

type PaginatedFileTransferAdapter interface {
	BrowseRemoteFilesPage(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string, cursor string) (RemoteFilePage, error)
}

type ErrorPresenter interface {
	WriteConnectorError(w http.ResponseWriter, err error) bool
	ConnectorErrorMessage(prefix string, err error) string
}

// CredentialResourceAdapter manages connector-owned credential resources.
type CredentialResourceAdapter interface {
	ListCredentialResources(w http.ResponseWriter, r *http.Request, runtime CredentialResourceRuntime)
	CreateCredentialResource(w http.ResponseWriter, r *http.Request, runtime CredentialResourceRuntime)
	ImportCredentialResource(w http.ResponseWriter, r *http.Request, runtime CredentialResourceRuntime)
	GetCredentialResource(w http.ResponseWriter, r *http.Request, runtime CredentialResourceRuntime)
	UpdateCredentialResource(w http.ResponseWriter, r *http.Request, runtime CredentialResourceRuntime)
	DeleteCredentialResource(w http.ResponseWriter, r *http.Request, runtime CredentialResourceRuntime)
}

// ConsoleRestartResult is the connector-neutral shape returned by live runtime
// adapters when a persistent session is closed and running requests are
// canceled.
type ConsoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}
