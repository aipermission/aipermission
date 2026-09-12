package gatewayworkspace

import (
	connectorstate "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/connectors"
	observationstate "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/observation"
	securitystate "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/security"
	storagestate "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
)

// The capability DTOs below are immutable projections bound once when a
// workspace opens. Infrastructure owners consume only their projection rather
// than resolving the complete workspace Runtime dynamically.
type AccessCapabilities struct {
	Storage    storagestate.Port
	Connectors connectorstate.Port
	Security   securitystate.Port
}

type ConnectorActionCapabilities struct {
	Storage    storagestate.Port
	Connectors connectorstate.Port
	Security   securitystate.Port
	Tag        func([]byte) (string, error)
}

type ConnectorManagementCapabilities struct {
	Storage    storagestate.Port
	Connectors connectorstate.Port
	Security   securitystate.Port
}

type ConnectorPortsCapabilities struct {
	Storage    storagestate.Port
	Connectors connectorstate.Port
	Security   securitystate.Port
}

type ObservationCapabilities struct {
	Storage     storagestate.Port
	Connectors  connectorstate.Port
	Security    securitystate.Port
	Observation observationstate.Port
}

type OperationsCapabilities struct {
	Storage    storagestate.Port
	Connectors connectorstate.Port
	Security   securitystate.Port
}

type VaultCapabilities struct {
	Storage    storagestate.Port
	Connectors connectorstate.Port
	Security   securitystate.Port
}
