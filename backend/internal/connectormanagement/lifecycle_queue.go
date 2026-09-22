package connectormanagement

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func queueLifecycleChange(ctx context.Context, tx *sql.Tx, change TargetLifecycleChange) error {
	return connectortargets.NewLifecycleFinalizationStore(tx).Queue(ctx, connectortargets.LifecycleFinalizationChange{
		TargetID: change.TargetID, ProfileID: change.ProfileID,
		StaleReason: change.StaleReason, UserMessage: change.UserMessage,
		IncludeRunning: change.IncludeRunning,
	})
}

func WriteCommittedLifecycleError(w http.ResponseWriter, err error) {
	if err != nil {
		log.Printf("connector mutation committed with pending lifecycle cleanup: %v", err)
	}
	httptransport.WriteErrorCode(
		w,
		http.StatusConflict,
		"connector mutation committed, but lifecycle cleanup is pending; lock and unlock the workspace to retry cleanup before using connectors",
		"connector_lifecycle_finalization_pending",
	)
}
