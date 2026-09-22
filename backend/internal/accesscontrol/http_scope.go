// Package accesscontrol owns API-token lifecycle and token-scoped authorization.
package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

var (
	ErrAuthorizationUnchanged = errors.New("authorization is unchanged")
	ErrVaultDeliveryCanceled  = errors.New("Vault delivery was canceled")
)

type MutationRunner func(
	context.Context,
	string,
	func() any,
	func(*sql.Tx) error,
) error

type Scope struct {
	Database                  *sql.DB
	Tokens                    *tokens.Store
	Registry                  connectors.Catalog
	ReusableTokens            func(context.Context) (bool, error)
	ReusableTokensForMutation func(context.Context, *sql.Tx) (bool, error)
	Mutate                    MutationRunner
	AcquireExclusive          func(context.Context) (func(), error)
	FinishTokenInvalidation   func(context.Context, int64, []int64)
}

type ScopeProvider func(http.ResponseWriter) (Scope, bool)

type HTTPHandlers struct{ scope ScopeProvider }

type scopeRequirements uint16

const (
	requireDatabase scopeRequirements = 1 << iota
	requireTokens
	requireRegistry
	requireReusableTokens
	requireReusableTokensForMutation
	requireMutation
	requireExclusive
	requireInvalidation

	requireAuthorizationMutation = requireMutation | requireExclusive | requireInvalidation
)

func NewHTTPHandlers(scope ScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

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
	valid = valid && (requirements&requireTokens == 0 || scope.Tokens != nil)
	valid = valid && (requirements&requireRegistry == 0 || scope.Registry != nil)
	valid = valid && (requirements&requireReusableTokens == 0 || scope.ReusableTokens != nil)
	valid = valid && (requirements&requireReusableTokensForMutation == 0 || scope.ReusableTokensForMutation != nil)
	valid = valid && (requirements&requireMutation == 0 || scope.Mutate != nil)
	valid = valid && (requirements&requireExclusive == 0 || scope.AcquireExclusive != nil)
	valid = valid && (requirements&requireInvalidation == 0 || scope.FinishTokenInvalidation != nil)
	if !valid {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	return scope, true
}
