package backup

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

var errImportAuthorization = errors.New("database import authorization changed")

func validateRestoreDestination(w http.ResponseWriter, name, password string) bool {
	for _, field := range []struct{ label, value string }{{"password", password}, {"name", name}} {
		if strings.TrimSpace(field.value) == "" {
			httptransport.WriteError(w, http.StatusBadRequest, "database "+field.label+" is required")
			return false
		}
	}
	return true
}

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
	return lease, check, true
}

// Hold the bounded operation slot during staging, and acquire the exclusive
// lifecycle lease only after the verified candidate is ready for publication.
func (component *Component) importCommit(ctx context.Context, lease *lifecycleOperationLease, revalidate func() bool) func() error {
	return func() error {
		release, err := component.dependencies.Lifecycle.AcquireMutationContext(ctx)
		if err != nil {
			return err
		}
		lease.releaseLifecycle = release
		if !revalidate() {
			return errImportAuthorization
		}
		return nil
	}
}
