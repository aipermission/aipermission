package api

import (
	"context"
	"database/sql"
	"net/http"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/console"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/servers"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
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
	servers        *servers.Store
	sshKeys        *sshkeys.Store
	tokens         *tokens.Store
	mux            *http.ServeMux
	mu             sync.RWMutex
	lifecycleMu    sync.RWMutex
	authLimiter    *authRateLimiter
	uiSessionMu    sync.RWMutex
	uiSessions     map[string]uiSessionRecord
}

type databaseRuntime struct {
	id               string
	path             string
	gatewaySecret    string
	database         *sql.DB
	vault            *vault.Vault
	servers          *servers.Store
	sshKeys          *sshkeys.Store
	tokens           *tokens.Store
	fileTransfers    *filetransfer.Store
	consoleSessions  *console.Manager
	transferMu       sync.Mutex
	transferCancels  map[int64]context.CancelFunc
	securityMu       sync.RWMutex
	securitySettings securitySettingsResponse
	securityLoaded   bool
	redactionMu      sync.RWMutex
	redactionRules   []compiledRedactionRule
	redactionLoaded  bool
	mcpMu            sync.RWMutex
	mcpStarted       bool
}

func NewServer(cfg config.Config, database *sql.DB, secretVault *vault.Vault, serverStore *servers.Store, sshKeyStore *sshkeys.Store, tokenStore *tokens.Store) *Server {
	activeID := dbpkg.DefaultDatabaseID(cfg.DataPath)
	server := &Server{
		config:         cfg,
		activeDataPath: cfg.DataPath,
		activeDatabase: activeID,
		workspaces:     map[string]*databaseRuntime{},
		database:       database,
		vault:          secretVault,
		servers:        serverStore,
		sshKeys:        sshKeyStore,
		tokens:         tokenStore,
		mux:            http.NewServeMux(),
		authLimiter:    newAuthRateLimiter(),
		uiSessions:     map[string]uiSessionRecord{},
	}
	runtime := &databaseRuntime{
		id:              activeID,
		path:            cfg.DataPath,
		gatewaySecret:   cfg.GatewaySecret,
		database:        database,
		vault:           secretVault,
		servers:         serverStore,
		sshKeys:         sshKeyStore,
		tokens:          tokenStore,
		fileTransfers:   filetransfer.NewStore(database),
		transferCancels: map[int64]context.CancelFunc{},
	}
	runtime.consoleSessions = console.NewManager(database, server.serverSSHMaterialForRuntime(runtime), server.knownHostsPath(), server.runtimeRedactor(runtime))
	server.workspaces[activeID] = runtime
	server.routes()
	return server
}

func NewLockedServer(cfg config.Config) *Server {
	server := &Server{
		config:         cfg,
		activeDataPath: cfg.DataPath,
		activeDatabase: dbpkg.DefaultDatabaseID(cfg.DataPath),
		workspaces:     map[string]*databaseRuntime{},
		mux:            http.NewServeMux(),
		authLimiter:    newAuthRateLimiter(),
		uiSessions:     map[string]uiSessionRecord{},
	}
	server.routes()
	return server
}
