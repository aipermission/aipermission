package api

import (
	"context"
	"database/sql"
	"net/http"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type Server struct {
	config         config.Config
	activeDataPath string
	activeDatabase string
	workspaces     map[string]*databaseRuntime
	database       *sql.DB
	vault          *vault.Vault
	tokens         *tokens.Store
	registry       *connectors.Registry
	mux            *http.ServeMux
	mu             sync.RWMutex
	lifecycleMu    sync.RWMutex
	authLimiter    *authRateLimiter
	uiSessionMu    sync.RWMutex
	uiSessions     map[string]uiSessionRecord
}

type databaseRuntime struct {
	id                 string
	path               string
	gatewaySecret      string
	database           *sql.DB
	vault              *vault.Vault
	tokens             *tokens.Store
	registry           *connectors.Registry
	connectorResources map[string]any
	fileTransfers      *filetransfer.Store
	consoleSessions    *console.Manager
	transferMu         sync.Mutex
	transferCancels    map[int64]context.CancelFunc
	batchCancels       map[int64]context.CancelFunc
	transferControls   map[int64]*transferControl
	batchControls      map[int64]*transferControl
	securityMu         sync.RWMutex
	securitySettings   securitySettingsResponse
	securityLoaded     bool
	redactionMu        sync.RWMutex
	redactionRules     []compiledRedactionRule
	redactionLoaded    bool
	mcpMu              sync.RWMutex
	mcpStarted         bool
}

type serverOptions struct {
	registry *connectors.Registry
}

type ServerOption func(*serverOptions)

func WithConnectorRegistry(registry *connectors.Registry) ServerOption {
	return func(options *serverOptions) {
		options.registry = registry
	}
}

func resolveServerOptions(options []ServerOption) serverOptions {
	resolved := serverOptions{registry: connectors.NewRegistry()}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	if resolved.registry == nil {
		resolved.registry = connectors.NewRegistry()
	}
	return resolved
}

func NewServer(cfg config.Config, database *sql.DB, secretVault *vault.Vault, tokenStore *tokens.Store, options ...ServerOption) *Server {
	activeID := dbpkg.DefaultDatabaseID(cfg.DataPath)
	resolved := resolveServerOptions(options)
	registry := resolved.registry
	server := &Server{
		config:         cfg,
		activeDataPath: cfg.DataPath,
		activeDatabase: activeID,
		workspaces:     map[string]*databaseRuntime{},
		database:       database,
		vault:          secretVault,
		tokens:         tokenStore,
		registry:       registry,
		mux:            http.NewServeMux(),
		authLimiter:    newAuthRateLimiter(),
		uiSessions:     map[string]uiSessionRecord{},
	}
	runtime := &databaseRuntime{
		id:                 activeID,
		path:               cfg.DataPath,
		gatewaySecret:      cfg.GatewaySecret,
		database:           database,
		vault:              secretVault,
		tokens:             tokenStore,
		registry:           registry,
		connectorResources: connectorRuntimeResources(registry, database, secretVault),
		fileTransfers:      filetransfer.NewStore(database),
		transferCancels:    map[int64]context.CancelFunc{},
		batchCancels:       map[int64]context.CancelFunc{},
		transferControls:   map[int64]*transferControl{},
		batchControls:      map[int64]*transferControl{},
	}
	runtime.consoleSessions = console.NewManager(database, server.runtimeConsoleOpener(runtime), server.runtimeRedactor(runtime))
	server.workspaces[activeID] = runtime
	server.routes()
	return server
}

func NewLockedServer(cfg config.Config, options ...ServerOption) *Server {
	resolved := resolveServerOptions(options)
	server := &Server{
		config:         cfg,
		activeDataPath: cfg.DataPath,
		activeDatabase: dbpkg.DefaultDatabaseID(cfg.DataPath),
		workspaces:     map[string]*databaseRuntime{},
		registry:       resolved.registry,
		mux:            http.NewServeMux(),
		authLimiter:    newAuthRateLimiter(),
		uiSessions:     map[string]uiSessionRecord{},
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

func (runtime *databaseRuntime) connectorRegistry() *connectors.Registry {
	if runtime != nil && runtime.registry != nil {
		return runtime.registry
	}
	return connectors.NewRegistry()
}
