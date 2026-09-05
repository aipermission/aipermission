package connectorapi

// FileTransferPathPolicy keeps remote identities separate from filesystem paths.
// Adapters without this capability retain the gateway's filesystem semantics.
type FileTransferPathPolicy interface {
	NormalizeTransferPath(value string, directory bool) (string, error)
	JoinTransferPath(directory, relative string) (string, error)
	ParentTransferPath(directory string) string
	ValidateDownloadPaths(paths []string) error
}
