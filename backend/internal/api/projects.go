package api

import (
	"net/http"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func handleProjectError(w http.ResponseWriter, err error) {
	projectstore.WriteHTTPError(w, err)
}
