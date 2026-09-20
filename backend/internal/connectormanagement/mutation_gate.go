package connectormanagement

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func acquireLifecycleMutation(
	w http.ResponseWriter,
	r *http.Request,
	acquire func(context.Context) (func(), error),
	canceledMessage string,
) (func(), bool) {
	release, err := acquire(r.Context())
	if err != nil {
		httptransport.WriteError(w, http.StatusRequestTimeout, canceledMessage)
		return nil, false
	}
	if release == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return release, true
}
