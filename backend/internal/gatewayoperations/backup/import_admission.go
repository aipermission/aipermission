package backup

import (
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

var errImportAuthorization = errors.New("database import authorization changed")

func (component *Component) admitImport(w http.ResponseWriter, r *http.Request) (*lifecycleOperationLease, func() bool, bool) {
	lease, err := component.acquireReadOperation(r.Context())
	if err != nil {
		httptransport.WriteError(w, http.StatusRequestTimeout, "database import admission was canceled")
		return nil, nil, false
	}
	if r.Context().Err() != nil {
		lease.Release()
		httptransport.WriteError(w, http.StatusRequestTimeout, "database import admission was canceled")
		return nil, nil, false
	}
	if component.dependencies.AuthorizeImport == nil {
		lease.Release()
		httptransport.WriteInternalError(w)
		return nil, nil, false
	}
	check, ok := component.dependencies.AuthorizeImport(w, r)
	if !ok || check == nil {
		lease.Release()
		if ok {
			httptransport.WriteInternalError(w)
		}
		return nil, nil, false
	}
	lease.ReleaseLifecycle()
	return lease, check, true
}
