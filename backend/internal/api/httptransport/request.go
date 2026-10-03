package httptransport

import (
	"net/http"

	transportcontract "github.com/aipermission/aipermission/backend/internal/httptransport"
)

const ConnectorActionJSONBodyBytes = 32 << 20

// DecodeJSON applies the API route budget through the shared strict decoder.
func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	limit := transportcontract.DefaultJSONBodyBytes
	if r.URL.Path == "/api/connector-actions/local-run" || r.URL.Path == "/api/mcp/connector-actions/call" {
		limit = ConnectorActionJSONBodyBytes
	}
	return transportcontract.DecodeJSON(w, r, target, limit)
}
