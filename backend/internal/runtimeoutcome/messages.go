// Package runtimeoutcome owns terminal messages shared by runtime shutdown and
// startup recovery.
package runtimeoutcome

const (
	ConnectorActionUnknown = "gateway restarted while the connector action was running; inspect the target state before retrying because the remote outcome is unknown"
	CommandCanceled        = "workspace locked while command was running"
	TransferInterrupted    = "workspace locked while file transfer was running"
	TransferQueueStopped   = "workspace locked while file transfer queue was running"
)
