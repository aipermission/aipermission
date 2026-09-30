package gatewayconnectormanagement

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func acquireTargetAdmission(w http.ResponseWriter, r *http.Request, storage StoragePorts, exclusive bool, canceledMessage string) (func(), bool) {
	acquire := storage.AcquireDelivery
	if exclusive {
		acquire = storage.AcquireExclusive
	}
	if acquire == nil || storage.Admission == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
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
	*r = *r.WithContext(connectors.WithDeliveryAdmission(r.Context(), storage.Admission))
	return release, true
}
