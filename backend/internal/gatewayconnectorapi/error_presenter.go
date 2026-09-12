package gatewayconnectorapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

func PresentedError(adapter any, err error) (ErrorPresentation, bool) {
	presenter, _ := adapter.(ErrorPresenter)
	if presenter == nil {
		return ErrorPresentation{}, false
	}
	return presenter.PresentConnectorError(err)
}

// WritePresentedError renders a connector-owned typed presentation at the
// HTTP boundary. It reports whether the adapter handled the response.
func WritePresentedError(w http.ResponseWriter, adapter any, err error) bool {
	presentation, ok := PresentedError(adapter, err)
	if !ok {
		return false
	}
	WriteErrorPresentation(w, presentation)
	return true
}

func WriteErrorPresentation(w http.ResponseWriter, presentation ErrorPresentation) {
	payload, err := MarshalErrorPresentation(presentation)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	WriteMarshaledErrorPresentation(w, presentation, payload)
}

func MarshalErrorPresentation(presentation ErrorPresentation) ([]byte, error) {
	return json.Marshal(presentation.Payload)
}

func WriteMarshaledErrorPresentation(w http.ResponseWriter, presentation ErrorPresentation, payload []byte) {
	for key, values := range presentation.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	status := presentation.StatusCode
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(payload, '\n'))
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
