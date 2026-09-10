// Package connectormanagement owns connector catalog and target/profile HTTP views.
package connectormanagement

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type ConnectorFeatures struct {
	LiveConsoleCapability string
	FileTransfer          bool
}

type Scope struct {
	Database                    *sql.DB
	Registry                    *connectors.Registry
	Features                    func(string) ConnectorFeatures
	SessionEnvironmentSupported func(context.Context, int64) bool
}

type ScopeProvider func(http.ResponseWriter) (Scope, bool)

type HTTPHandlers struct{ scope ScopeProvider }

type scopeRequirements uint8

const (
	requireDatabase scopeRequirements = 1 << iota
	requireRegistry
	requireFeatures
	requireSessionEnvironment
)

func NewHTTPHandlers(scope ScopeProvider) *HTTPHandlers { return &HTTPHandlers{scope: scope} }

func (h *HTTPHandlers) resolve(w http.ResponseWriter, requirements scopeRequirements) (Scope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return Scope{}, false
	}
	valid := requirements&requireDatabase == 0 || scope.Database != nil
	valid = valid && (requirements&requireRegistry == 0 || scope.Registry != nil)
	valid = valid && (requirements&requireFeatures == 0 || scope.Features != nil)
	valid = valid && (requirements&requireSessionEnvironment == 0 || scope.SessionEnvironmentSupported != nil)
	if !valid {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	return scope, true
}
