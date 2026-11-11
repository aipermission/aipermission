package security

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type State struct {
	policy        *securitypolicy.Service
	runtime       runtimecontrol.State
	vaultLeases   *vaultsessions.Store
	vaultDelivery vaultsessions.DeliveryCoordinator
}

func New(database *sql.DB) *State {
	state := &State{policy: securitypolicy.NewService(database), vaultLeases: vaultsessions.NewStore()}
	waits := &sqldb.PoolWaitReporter{}
	state.vaultDelivery.SetDeliveryGuard(func(ctx context.Context) error {
		if err := connectortargets.NewLifecycleFinalizationStore(database).RequireReady(ctx); err != nil {
			waits.Report(database, err)
			return err
		}
		err := vaultfinalization.NewStore(database).RequireReady(ctx)
		waits.Report(database, err)
		return err
	})
	return state
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
