// Package security defines the workspace boundary's security-state port.
package security

import (
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type Port interface {
	PolicyService() *securitypolicy.Service
	RuntimeControlState() *runtimecontrol.State
	VaultLeaseStore() *vaultsessions.Store
	VaultDeliveryCoordinator() *vaultsessions.DeliveryCoordinator
}
