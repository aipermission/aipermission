// Package runtimecontract owns the encrypted workspace runtime boundary.
package runtimecontract

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/componentstate"
	gatewayconnectors "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/connectors"
	gatewayobservation "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/observation"
	gatewaysecurity "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/security"
	gatewaystorage "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type Runtime interface {
	WorkspaceIdentity() workspacelifecycle.Identity
	WorkspaceDatabase() *sql.DB
	StoragePort() gatewaystorage.Port
	ConnectorPort() gatewayconnectors.Port
	ComponentStatePort() componentstate.Port
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
	IdentityReady() bool
	IsMCPStarted() bool
	SetMCPStarted(bool)
}
