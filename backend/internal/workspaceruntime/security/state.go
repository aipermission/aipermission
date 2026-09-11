package security

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type State struct {
	Policy        *securitypolicy.Service
	Runtime       runtimecontrol.State
	VaultLeases   *vaultsessions.Store
	VaultDelivery vaultsessions.DeliveryCoordinator
}

type Port interface {
	PolicyService() *securitypolicy.Service
	RuntimeControlState() *runtimecontrol.State
	VaultLeaseStore() *vaultsessions.Store
	VaultDeliveryCoordinator() *vaultsessions.DeliveryCoordinator
}

func New(database *sql.DB) State {
	return State{
		Policy:      securitypolicy.NewService(database),
		VaultLeases: vaultsessions.NewStore(),
	}
}

func (s *State) PolicyService() *securitypolicy.Service {
	if s == nil {
		return nil
	}
	return s.Policy
}

func (s *State) RuntimeControlState() *runtimecontrol.State {
	if s == nil {
		return nil
	}
	return &s.Runtime
}

func (s *State) VaultLeaseStore() *vaultsessions.Store {
	if s == nil {
		return nil
	}
	return s.VaultLeases
}

func (s *State) VaultDeliveryCoordinator() *vaultsessions.DeliveryCoordinator {
	if s == nil {
		return nil
	}
	return &s.VaultDelivery
}
