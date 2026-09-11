// Package gatewayadapter adapts the concrete workspace runtime to the
// gateway-owned runtime contract.
package gatewayadapter

import (
	"github.com/aipermission/aipermission/backend/internal/componentstate"
	gatewayconnectors "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/connectors"
	gatewayobservation "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/observation"
	gatewaysecurity "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/security"
	gatewaystorage "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimecontract"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type adapter struct{ *workspaceruntime.Runtime }

func Wrap(runtime *workspaceruntime.Runtime) runtimecontract.Runtime {
	if runtime == nil {
		return nil
	}
	return &adapter{Runtime: runtime}
}

func Unwrap(runtime runtimecontract.Runtime) (*workspaceruntime.Runtime, bool) {
	if runtime == nil {
		return nil, true
	}
	value, ok := runtime.(*adapter)
	if !ok {
		return nil, false
	}
	if value == nil {
		return nil, true
	}
	return value.Runtime, true
}

func (runtime *adapter) StoragePort() gatewaystorage.Port {
	return runtime.Runtime.StoragePort()
}

func (runtime *adapter) ConnectorPort() gatewayconnectors.Port {
	return runtime.Runtime.ConnectorPort()
}

func (runtime *adapter) ComponentStatePort() componentstate.Port {
	return runtime.Runtime.ComponentStatePort()
}

func (runtime *adapter) SecurityPort() gatewaysecurity.Port {
	return runtime.Runtime.SecurityPort()
}

func (runtime *adapter) ObservationPort() gatewayobservation.Port {
	return runtime.Runtime.ObservationPort()
}

var _ runtimecontract.Runtime = (*adapter)(nil)
