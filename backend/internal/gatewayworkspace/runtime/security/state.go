package security

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type State struct {
	policy        *securitypolicy.Service
	runtime       runtimecontrol.State
	vaultLeases   *vaultsessions.Store
	vaultDelivery vaultsessions.DeliveryCoordinator
}

func New(database *sql.DB) State {
	return State{policy: securitypolicy.NewService(database), vaultLeases: vaultsessions.NewStore()}
}

func (s *State) PolicyService() *securitypolicy.Service {
	if s == nil {
		return nil
	}
	return s.policy
}

func (s *State) RuntimeControlState() *runtimecontrol.State {
	if s == nil {
		return nil
	}
	return &s.runtime
}

func (s *State) VaultLeaseStore() *vaultsessions.Store {
	if s == nil {
		return nil
	}
	return s.vaultLeases
}

func (s *State) VaultDeliveryCoordinator() *vaultsessions.DeliveryCoordinator {
	if s == nil {
		return nil
	}
	return &s.vaultDelivery
}

var _ Port = (*State)(nil)
