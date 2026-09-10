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

func New(database *sql.DB) State {
	return State{
		Policy:      securitypolicy.NewService(database),
		VaultLeases: vaultsessions.NewStore(),
	}
}
