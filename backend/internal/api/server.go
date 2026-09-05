package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type Server struct {
	config               config.Config
	activeDataPath       string
	activeDatabase       string
	workspaces           map[string]*databaseRuntime
	database             *sql.DB
	vault                *vault.Vault
	tokens               *tokens.Store
	registry             *connectors.Registry
	adapterRegistry      *connectorapi.Registry
	mux                  *http.ServeMux
	mu                   sync.RWMutex
	lifecycleMu          sync.RWMutex
	maintenanceConsole   *maintenanceConsoleRuntime
	authLimiter          *authRateLimiter
	mcpIPAuthLimiter     *authRateLimiter
	mcpTokenAuthLimiter  *authRateLimiter
	vaultRevealLimiter   *windowRateLimiter
	vaultGenerateLimiter *windowRateLimiter
	vaultRequestLimiter  *windowRateLimiter
	uiSessionMu          sync.RWMutex
	uiSessions           map[string]uiSessionRecord
	auditHealth          auditHealthState
	databaseMove         func(string, string) error
	databasePublish      func(string, string) error
	runtimeOpen          func(string, string, string) (*databaseRuntime, error)
	retentionInterval    time.Duration
	backupOperationMu    sync.Mutex
	backupOperations     chan struct{}
}

type databaseRuntime struct {
	id                 string
	path               string
	gatewaySecret      string
	database           *sql.DB
	vault              *vault.Vault
	tokens             *tokens.Store
	registry           *connectors.Registry
	adapterRegistry    *connectorapi.Registry
	connectorResources connectorruntime.ResourceScopes
	fileTransfers      *filetransfer.Store
	consoleSessions    *console.Manager
	transferJobs       transferjobs.Registry
	securityMu         sync.RWMutex
	securitySettings   securitySettingsResponse
	securityLoaded     bool
	redactionMu        sync.RWMutex
	redactionRules     []compiledRedactionRule
	redactionLoaded    bool
	credBoundaryMu     sync.RWMutex
	credBoundaries     map[int64]connectorCredentialBoundary
	mcpMu              sync.RWMutex
	mcpStarted         bool
	workspaceUUID      string
	uiRetryIdentity    string
	runtimeInstanceID  string
	actionIdentityKey  []byte
	vaultLeases        *vaultsessions.Store
	vaultDelivery      vaultDeliveryCoordinator
	vaultPreviewMu     sync.Mutex
	vaultPreviewNonces map[int64]string
	identityMu         sync.Mutex
	auditDispatcher    *auditoutbox.Dispatcher
	retentionMu        sync.Mutex
	retentionCancel    context.CancelFunc
	retentionDone      chan struct{}
	actionRecovery     connectorActionRecoveryWorker
	databaseOwnership  *dbpkg.DatabaseOwnership
}

type serverOptions struct {
	registry                   *connectors.Registry
	adapterRegistry            *connectorapi.Registry
	runtimeInstanceIDGenerator func() (string, error)
}

type ServerOption func(*serverOptions)

func WithConnectorRegistry(registry *connectors.Registry) ServerOption {
	return func(options *serverOptions) {
		options.registry = registry
	}
}

func WithConnectorAdapterRegistry(registry *connectorapi.Registry) ServerOption {
	return func(options *serverOptions) {
		options.adapterRegistry = registry
	}
}

func withRuntimeInstanceIDGenerator(generator func() (string, error)) ServerOption {
	return func(options *serverOptions) {
		options.runtimeInstanceIDGenerator = generator
	}
}

func resolveServerOptions(options []ServerOption) serverOptions {
	resolved := serverOptions{
		registry:                   connectors.NewRegistry(),
		adapterRegistry:            connectorapi.NewRegistry(),
		runtimeInstanceIDGenerator: executionprincipal.NewRuntimeInstanceID,
	}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	if resolved.registry == nil {
		resolved.registry = connectors.NewRegistry()
	}
	if resolved.adapterRegistry == nil {
		resolved.adapterRegistry = connectorapi.NewRegistry()
	}
	if resolved.runtimeInstanceIDGenerator == nil {
		resolved.runtimeInstanceIDGenerator = executionprincipal.NewRuntimeInstanceID
	}
	return resolved
}

func NewServer(cfg config.Config, database *sql.DB, secretVault *vault.Vault, tokenStore *tokens.Store, options ...ServerOption) (*Server, error) {
	scavengeDatabaseTempPaths(cfg.DataPath, time.Now())
	activeID := dbpkg.DefaultDatabaseID(cfg.DataPath)
	resolved := resolveServerOptions(options)
	registry := resolved.registry
	server := &Server{
		config:               cfg,
		activeDataPath:       cfg.DataPath,
		activeDatabase:       activeID,
		workspaces:           map[string]*databaseRuntime{},
		database:             database,
		vault:                secretVault,
		tokens:               tokenStore,
		registry:             registry,
		adapterRegistry:      resolved.adapterRegistry,
		mux:                  http.NewServeMux(),
		maintenanceConsole:   newMaintenanceConsoleRuntime(),
		authLimiter:          newAuthRateLimiter(),
		mcpIPAuthLimiter:     newMCPGlobalAuthRateLimiter(),
		mcpTokenAuthLimiter:  newAuthRateLimiter(),
		vaultRevealLimiter:   newWindowRateLimiter(8, time.Minute),
		vaultGenerateLimiter: newWindowRateLimiter(10, time.Minute),
		vaultRequestLimiter:  newWindowRateLimiter(30, time.Minute),
		uiSessions:           map[string]uiSessionRecord{},
		retentionInterval:    defaultRetentionCleanupInterval,
	}
	runtime := &databaseRuntime{
		id:              activeID,
		path:            cfg.DataPath,
		gatewaySecret:   cfg.GatewaySecret,
		database:        database,
		vault:           secretVault,
		tokens:          tokenStore,
		registry:        registry,
		adapterRegistry: resolved.adapterRegistry,
		fileTransfers:   filetransfer.NewStore(database),
		credBoundaries:  map[int64]connectorCredentialBoundary{},
		vaultLeases:     vaultsessions.NewStore(),
	}
	var err error
	runtime.workspaceUUID, err = projectvault.EnsureWorkspaceUUID(context.Background(), database)
	if err != nil {
		return nil, fmt.Errorf("initialize workspace identity: %w", err)
	}
	runtime.uiRetryIdentity, err = projectvault.EnsureUIRetryIdentity(context.Background(), database)
	if err != nil {
		return nil, fmt.Errorf("initialize UI retry identity: %w", err)
	}
	runtime.actionIdentityKey, err = deriveConnectorActionIdentityKey(cfg.GatewaySecret, runtime.workspaceUUID)
	if err != nil {
		return nil, fmt.Errorf("initialize connector action identity: %w", err)
	}
	runtime.connectorResources = connectorruntime.NewResourceScopes(database, secretVault, runtime.workspaceUUID)
	runtime.runtimeInstanceID, err = resolved.runtimeInstanceIDGenerator()
	if err != nil {
		return nil, fmt.Errorf("initialize runtime identity: %w", err)
	}
	runtime.consoleSessions = console.NewManager(database, server.runtimeConsoleOpener(runtime), server.runtimeRedactor(runtime))
	server.configureVaultSessionRuntime(runtime)
	server.configureAuditDispatcher(runtime)
	server.workspaces[activeID] = runtime
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}

func NewLockedServer(cfg config.Config, options ...ServerOption) *Server {
	scavengeDatabaseTempPaths(cfg.DataPath, time.Now())
	resolved := resolveServerOptions(options)
	server := &Server{
		config:               cfg,
		activeDataPath:       cfg.DataPath,
		activeDatabase:       dbpkg.DefaultDatabaseID(cfg.DataPath),
		workspaces:           map[string]*databaseRuntime{},
		registry:             resolved.registry,
		adapterRegistry:      resolved.adapterRegistry,
		mux:                  http.NewServeMux(),
		maintenanceConsole:   newMaintenanceConsoleRuntime(),
		authLimiter:          newAuthRateLimiter(),
		mcpIPAuthLimiter:     newMCPGlobalAuthRateLimiter(),
		mcpTokenAuthLimiter:  newAuthRateLimiter(),
		vaultRevealLimiter:   newWindowRateLimiter(8, time.Minute),
		vaultGenerateLimiter: newWindowRateLimiter(10, time.Minute),
		vaultRequestLimiter:  newWindowRateLimiter(30, time.Minute),
		uiSessions:           map[string]uiSessionRecord{},
		retentionInterval:    defaultRetentionCleanupInterval,
	}
	server.routes()
	return server
}

func (s *Server) connectorRegistry() *connectors.Registry {
	if s != nil && s.registry != nil {
		return s.registry
	}
	return connectors.NewRegistry()
}

func (s *Server) connectorAdapterRegistry() *connectorapi.Registry {
	if s != nil && s.adapterRegistry != nil {
		return s.adapterRegistry
	}
	return connectorapi.NewRegistry()
}

func (runtime *databaseRuntime) connectorRegistry() *connectors.Registry {
	if runtime != nil && runtime.registry != nil {
		return runtime.registry
	}
	return connectors.NewRegistry()
}

func (runtime *databaseRuntime) connectorAdapterRegistry() *connectorapi.Registry {
	if runtime != nil && runtime.adapterRegistry != nil {
		return runtime.adapterRegistry
	}
	return connectorapi.NewRegistry()
}
