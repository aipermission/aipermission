package gatewayconnectorapi

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func PresentedErrorMessage(adapter any, prefix string, err error) string {
	return connectorapi.PresentedErrorMessage(adapter, prefix, err)
}

func ErrorCode(err error) string { return connectors.ErrorCode(err) }

func ErrorStatus(err error) connectors.ResultStatus { return connectors.ErrorStatus(err) }
