// Package runtimecontract owns the encrypted workspace runtime boundary.
package runtimecontract

import (
	"database/sql"

	gatewayconnectors "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/connectors"
	gatewayobservation "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/observation"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/operations"
	gatewaysecurity "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/security"
	gatewaystorage "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type Runtime interface {
	WorkspaceIdentity() workspacelifecycle.Identity
	WorkspaceDatabase() *sql.DB
	StoragePort() gatewaystorage.Port
	ConnectorPort() gatewayconnectors.Port
	OperationsPort() gatewayoperations.Port
	SecurityPort() gatewaysecurity.Port
	ObservationPort() gatewayobservation.Port
	WorkspaceIdentifier() string
	RuntimeIdentifier() string
	DatabaseIdentifier() string
	DatabasePath() string
	GatewaySecretValue() string
	UIRetryIdentifier() string
	ActionIdentity() []byte
	ClearActionIdentity()
	EnsureIdentity(func(*sql.DB) (string, error), func() (string, error)) error
	IsMCPStarted() bool
	SetMCPStarted(bool)
}
