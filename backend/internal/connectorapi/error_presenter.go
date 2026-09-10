package connectorapi

import (
	"net/http"
	"strings"
)

// WritePresentedError lets an adapter project a connector-specific error
// response. It reports whether the adapter handled the response.
func WritePresentedError(w http.ResponseWriter, adapter any, err error) bool {
	presenter, _ := adapter.(ErrorPresenter)
	if presenter == nil {
		return false
	}
	return presenter.WriteConnectorError(w, err)
}

// PresentedErrorMessage returns an adapter-specific error message when one is
// available and otherwise preserves the common prefix and underlying error.
func PresentedErrorMessage(adapter any, prefix string, err error) string {
	presenter, _ := adapter.(ErrorPresenter)
	if presenter != nil {
		return presenter.ConnectorErrorMessage(prefix, err)
	}
	if err == nil {
		return prefix
	}
	return prefix + ": " + strings.TrimSpace(err.Error())
}
