package gatewayconnectorapi

import "github.com/aipermission/aipermission/backend/internal/connectors"

func FormatTargetRef(connectorKind string, targetID int64, profileID int64) string {
	return connectors.FormatTargetRef(connectorKind, targetID, profileID)
}
