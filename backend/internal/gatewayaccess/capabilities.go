package gatewayaccess

import (
	"context"
	"database/sql"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
)

func (component *Component) CanReadVaultMetadata(
	ctx context.Context,
	database *sql.DB,
	tokenID int64,
	projectID int64,
	now time.Time,
) (bool, error) {
	if component == nil || database == nil {
		return false, ErrComponentUnavailable
	}
	capability, err := accesscontrol.NewCapabilityStore(database).Effective(
		ctx, tokenID, projectID, accesscontrol.VaultMetadataRead, now,
	)
	return err == nil && capability.ExecutionRule == accesscontrol.RuleAlwaysRun, err
}
