package gatewayconnectorapi

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
)

func NewLiveConsoleHTTPHandlers(scope connectorapi.LiveConsoleHTTPScopeProvider) *connectorapi.LiveConsoleHTTPHandlers {
	return connectorapi.NewLiveConsoleHTTPHandlers(scope)
}
