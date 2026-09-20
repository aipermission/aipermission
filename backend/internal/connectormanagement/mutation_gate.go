package connectormanagement

import (
	"context"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const lifecycleFinalizationTimeout = 10 * time.Second

func acquireLifecycleMutation(
	w http.ResponseWriter,
	r *http.Request,
	acquire func(context.Context) (func(), error),
	canceledMessage string,
) (func(), bool) {
	release, err := acquire(r.Context())
	if err != nil {
		if release != nil {
			release()
		}
		httptransport.WriteError(w, http.StatusRequestTimeout, canceledMessage)
		return nil, false
	}
	if release == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return release, true
}

func finalizeLifecycleMutation(requestCtx context.Context, finalize func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(requestCtx), lifecycleFinalizationTimeout)
	defer cancel()
	return finalize(ctx)
}
