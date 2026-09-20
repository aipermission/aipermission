// Package gatewayconnectorapi owns the optional gateway adapter registry for
// connector capabilities that cannot be implemented by the structured action
// interface alone.
package gatewayconnectorapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	transportcontract "github.com/aipermission/aipermission/backend/internal/httptransport"
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
	EnsureRuntimeSurface(ctx context.Context, input EnsureRuntimeSurfaceInput) (RuntimeSurface, error)
	ListRuntimeSurfacesForProfile(ctx context.Context, targetID int64, profileID int64, capabilityKind string) ([]RuntimeSurface, error)
	TargetProfileByRuntimeID(ctx context.Context, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, RuntimeSurface, error)
	ListCredentialProfiles(ctx context.Context, targetID int64) ([]connectors.CredentialProfileView, error)
	CredentialResources(resourceKind string) CredentialResourceStore
}

// LiveSessionRuntime exposes the generic persistent console manager.
type ConsoleSessionRuntime interface {
	EnsureReady(ctx context.Context, principal Principal, runtimeID int64) (ConsoleSessionHandle, error)
	Exec(ctx context.Context, principal Principal, runtimeID int64, command string) (ConsoleExecResult, error)
	ActiveSnapshot(ctx context.Context, principal Principal, runtimeID int64) (ConsoleRecord, error)
	WaitActive(ctx context.Context, principal Principal, handle ConsoleSessionHandle) (ConsoleExecResult, error)
	InterruptActive(ctx context.Context, principal Principal, handle ConsoleSessionHandle) error
}

type LiveSessionRuntime interface {
	ConnectorDataRuntime
	ConnectorConsoleSessions() ConsoleSessionRuntime
}

// PrincipalRuntime resolves the local human execution principal.
type PrincipalRuntime interface {
	ConnectorLocalExecutionPrincipal() (Principal, error)
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
	ResolveRuntimeContext(ctx context.Context, runtimeID int64, capabilityKind string) (connectors.RuntimeContext, RuntimeSurface, error)
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

// RoutePolicy declares the local UI authorization class a route expects.
type RoutePolicy string

const (
	RoutePolicyUIRead      RoutePolicy = "ui_read"
	RoutePolicyUIMutation  RoutePolicy = "ui_mutation"
	CoreCredentialsSegment             = transportcontract.ConnectorCredentialsSegment
)

// RouteDefinition is the canonical runtime and documentation contract for a
// connector-owned HTTP route.
type RouteDefinition struct {
	Kind            string
	Method          string
	Path            string
	Policy          RoutePolicy
	ReadHandler     func(ReadRouteGateway, http.ResponseWriter, *http.Request)
	MutationHandler func(MutationRouteGateway, http.ResponseWriter, *http.Request)
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
	ConnectorRunCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error)
	ConnectorOpenLiveConsole(ctx context.Context, targetRef string, rows int, cols int, params map[string]any) (*LiveConsoleSession, error)
}

type LiveConsoleOpenRequest struct {
	RuntimeID      int64
	Generation     int64
	Rows           int
	Cols           int
	Params         map[string]any
	HasEnvironment bool
}

type LiveConsoleSession struct {
	Stdin  io.WriteCloser
	Output <-chan console.RuntimeOutput
	// Output and Done are transport-owned. Close must stop the producers, wait
	// for their pumps, close Output, and then report completion through Done.
	Done                     <-chan error
	Resize                   func(cols int, rows int) error
	Close                    func() error
	ApplyEnvironment         func(context.Context, SessionEnvironment) error
	PeerIdentity             string
	StartupInputAfterConnect string
}

// ConsoleRestartGateway owns cancellation and invalidation for one persistent
// connector console session.
type ConsoleRestartGateway interface {
	ConnectorRestartConsoleSession(ctx context.Context, principal Principal, runtimeID int64, runningRequestError string) (ConsoleRestartResult, error)
}

// ActionFinishGateway owns completion of an asynchronous connector action.
type ActionFinishGateway interface {
	ConnectorFinishActionRequest(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (ActionRequest, error)
}

// TransferBatchGateway owns creation and execution of connector download jobs.
type TransferBatchGateway interface {
	ConnectorCreateAndRunDownloadBatch(ctx context.Context, authorization TransferAuthorization, runtimeID int64, remotePaths []string, archiveName string, source string) (TransferBatch, error)
}

// TransferAuthorization binds an asynchronous transfer to the immutable
// target/profile snapshot that passed connector action authorization.
type TransferAuthorization struct {
	ConnectorKind         string
	TargetID              int64
	TargetRef             string
	TargetUpdatedAt       string
	ProfileID             int64
	ProfileUpdatedAt      string
	ProfileSecretRevision string
}

type TransferBatch struct {
	ID        int64
	Status    string
	ItemCount int
}

// RuntimeCapabilityGateway exposes connector-owned runtime capabilities to
// adapters executing outside the structured action pipeline.
type RuntimeCapabilityGateway interface {
	ConnectorRuntimeCapabilities() connectors.RuntimeCapabilityResolver
}

type ReadRouteGateway interface {
	RuntimeAvailabilityGateway
	PeerIdentityGateway
}

type MutationRouteGateway interface {
	ReadRouteGateway
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
	ConnectorDeleteTargetRecord(ctx context.Context, target Target, payload map[string]any) error
	ConnectorFinalizeDeletedTarget(ctx context.Context, target Target, staleReason string, payload map[string]any) (int64, error)
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

// Catalog is the immutable adapter lookup surface used after composition.
// Registration belongs only to the bootstrap Registry builder.
type Catalog interface {
	For(kind string) Adapter
	Kinds() []string
	RouteDefinitions(kinds []string) ([]RouteDefinition, error)
}

type catalogSnapshot struct {
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
	if !connectors.ValidIdentifier(kind) {
		return fmt.Errorf("invalid connector adapter kind %q", kind)
	}
	if isNilAdapter(adapter) {
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
	return adapterKinds(r.adapters)
}

// Snapshot returns an immutable copy suitable for runtime composition.
func (r *Registry) Snapshot() Catalog {
	if r == nil {
		return catalogSnapshot{adapters: map[string]Adapter{}}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapters := make(map[string]Adapter, len(r.adapters))
	for kind, adapter := range r.adapters {
		adapters[kind] = adapter
	}
	return catalogSnapshot{adapters: adapters}
}

// SnapshotCatalog copies any read-only catalog into an immutable runtime view.
func SnapshotCatalog(source Catalog) (Catalog, error) {
	if source == nil {
		return catalogSnapshot{adapters: map[string]Adapter{}}, nil
	}
	if registry, ok := source.(*Registry); ok {
		return registry.Snapshot(), nil
	}
	kinds := source.Kinds()
	adapters := make(map[string]Adapter, len(kinds))
	for _, rawKind := range kinds {
		kind := strings.TrimSpace(rawKind)
		if !connectors.ValidIdentifier(kind) {
			return nil, fmt.Errorf("invalid connector adapter kind %q", kind)
		}
		if kind == "" || kind != rawKind {
			return nil, fmt.Errorf("connector adapter catalog contains invalid kind %q", rawKind)
		}
		adapter := source.For(kind)
		if isNilAdapter(adapter) {
			return nil, fmt.Errorf("connector adapter catalog kind %q is missing", kind)
		}
		if _, exists := adapters[kind]; exists {
			return nil, fmt.Errorf("connector adapter catalog kind %q is duplicated", kind)
		}
		adapters[kind] = adapter
	}
	return catalogSnapshot{adapters: adapters}, nil
}

func isNilAdapter(adapter Adapter) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (snapshot catalogSnapshot) For(kind string) Adapter {
	return snapshot.adapters[strings.TrimSpace(kind)]
}

func (snapshot catalogSnapshot) Kinds() []string {
	return adapterKinds(snapshot.adapters)
}

func (snapshot catalogSnapshot) RouteDefinitions(kinds []string) ([]RouteDefinition, error) {
	return routeDefinitions(snapshot.For, kinds)
}

func adapterKinds(adapters map[string]Adapter) []string {
	kinds := make([]string, 0, len(adapters))
	for kind := range adapters {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// RouteDefinitions returns validated connector-owned routes for the requested
// connector kinds. The result is deterministic so runtime registration,
// generated contracts, and tests share one inventory.
func (r *Registry) RouteDefinitions(kinds []string) ([]RouteDefinition, error) {
	return routeDefinitions(r.For, kinds)
}

func routeDefinitions(adapterFor func(string) Adapter, kinds []string) ([]RouteDefinition, error) {
	routes := []RouteDefinition{}
	seen := map[string]string{}
	for _, rawKind := range kinds {
		kind := strings.TrimSpace(rawKind)
		adapter, _ := adapterFor(kind).(RouteRegistrar)
		if adapter == nil {
			continue
		}
		for _, route := range adapter.Routes() {
			route.Kind = kind
			route.Method = strings.ToUpper(strings.TrimSpace(route.Method))
			route.Path = strings.TrimSpace(route.Path)
			if route.Method == "" {
				return nil, fmt.Errorf("connector adapter %q route method is required", kind)
			}
			if !strings.HasPrefix(route.Path, "/") {
				return nil, fmt.Errorf("connector adapter %q route path %q must start with /", kind, route.Path)
			}
			if err := validateRoutePolicy(kind, route); err != nil {
				return nil, fmt.Errorf("connector adapter %q route %s %s: %w", kind, route.Method, route.Path, err)
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

func validateRoutePolicy(kind string, route RouteDefinition) error {
	if err := ValidateConnectorOwnedRoutePath(kind, route.Path); err != nil {
		return err
	}
	switch route.Policy {
	case RoutePolicyUIRead:
		if route.Method != http.MethodGet && route.Method != http.MethodHead {
			return fmt.Errorf("ui_read policy requires GET or HEAD")
		}
		if route.ReadHandler == nil {
			return fmt.Errorf("ui_read route has no read handler")
		}
		if route.MutationHandler != nil {
			return fmt.Errorf("ui_read route must not expose a mutation handler")
		}
	case RoutePolicyUIMutation:
		switch route.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			return fmt.Errorf("ui_mutation policy requires a state-changing method")
		}
		if route.MutationHandler == nil {
			return fmt.Errorf("ui_mutation route has no mutation handler")
		}
		if route.ReadHandler != nil {
			return fmt.Errorf("ui_mutation route must not expose a read handler")
		}
	default:
		return fmt.Errorf("route policy is required")
	}
	return nil
}

// ValidateConnectorOwnedRoutePath rejects paths outside an adapter's static
// namespace and paths reserved by the generic connector credential API.
func ValidateConnectorOwnedRoutePath(kind string, routePath string) error {
	return transportcontract.ValidateConnectorOwnedRoutePath(kind, routePath)
}

// RuntimeAdapter lets a connector provide gateway-owned async/runtime services.
type RuntimeAdapter interface {
	RuntimeCapabilities(server RuntimeActionGateway, runtime ActionRuntime) map[string]connectors.RuntimeCapability
	SupportsRunning(prepared connectors.RuntimeActionContext) bool
	FinishRunning(context.Context, ActionFinishGateway, ActionRuntime, int64, connectors.RuntimeActionContext, Principal, connectors.ActionHandles) error
	RunningHint(request ActionRequest) string
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
	TestDraft(context.Context, PeerIdentityGateway, ConnectorDataRuntime, any) (connectors.ManagementResponse, error)
}

// TargetDeleter lets a connector customize deletion behavior.
type TargetDeleter interface {
	// DeleteTarget writes connector-owned pre-commit responses itself. A returned
	// error means the target mutation committed but lifecycle finalization did not.
	DeleteTarget(handler TargetDeletionGateway, w http.ResponseWriter, r *http.Request, runtime TargetLifecycleRuntime, target Target) error
}

// CredentialProfileLifecycleAdapter lets a connector react to profile lifecycle
// changes without putting connector-specific branches in the core handlers.
type CredentialProfileLifecycleAdapter interface {
	BeforeCreateCredentialProfile(ctx context.Context, runtime TargetLifecycleRuntime, target Target) error
	BeforeDeleteCredentialProfile(ctx context.Context, handler ConsoleRestartGateway, runtime TargetLifecycleRuntime, target Target, profile CredentialProfile) error
}

// CredentialProfileTester lets a connector test an existing profile.
type CredentialProfileTester interface {
	TestCredentialProfile(context.Context, PeerIdentityGateway, ConnectorDataRuntime, connectors.TargetView, connectors.CredentialProfileView) (connectors.ManagementResponse, error)
}

// TargetOperationRunner runs connector-specific target operations.
type TargetOperationRunner interface {
	RunTargetOperation(context.Context, TargetOperationGateway, ConnectorDataRuntime, Target, string, any) (connectors.ManagementResponse, error)
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
	OpenLiveConsole(ctx context.Context, server LiveConsoleGateway, runtime LiveConsoleRuntime, request LiveConsoleOpenRequest) (*LiveConsoleSession, error)
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

type RemoteFilePage struct {
	Entries    []connectors.RemoteFileEntry `json:"entries"`
	NextCursor string                       `json:"next_cursor,omitempty"`
	HasMore    bool                         `json:"has_more"`
}

type FileTransferAdapter interface {
	BrowseRemoteFiles(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string) ([]connectors.RemoteFileEntry, error)
	StatRemotePath(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string) (connectors.RemotePathStatus, error)
	UploadFile(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options connectors.TransferOptions) (connectors.TransferResult, error)
	DownloadFile(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string, localPath string, options connectors.TransferOptions) (connectors.TransferResult, error)
}

// RemoteStagingRecoveryAdapter removes connector-owned upload staging after a
// gateway restart. Core persists only the opaque staging reference.
type RemoteStagingRecoveryAdapter interface {
	CleanupRemoteStaging(context.Context, FileTransferGateway, TransferRuntime, int64, string) error
}

type RecursiveFileTransferAdapter interface {
	ListRecursiveFiles(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string, maxItems int, maxObjectBytes int64, maxBatchBytes int64) ([]connectors.RemoteFileEntry, error)
}

type PaginatedFileTransferAdapter interface {
	BrowseRemoteFilesPage(ctx context.Context, server FileTransferGateway, runtime TransferRuntime, runtimeID int64, remotePath string, cursor string) (RemoteFilePage, error)
}

type ErrorPresenter interface {
	PresentConnectorError(err error) (ErrorPresentation, bool)
	ConnectorErrorMessage(prefix string, err error) string
}

type ErrorPresentation struct {
	StatusCode int
	Header     http.Header
	Payload    any
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
