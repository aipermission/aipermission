package api

import (
	"net/http"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func handleProjectError(w http.ResponseWriter, err error) {
	gatewayvault.WriteProjectHTTPError(w, err)
}
