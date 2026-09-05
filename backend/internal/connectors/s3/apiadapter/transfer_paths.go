package apiadapter

import s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"

func (adapter) NormalizeTransferPath(value string, directory bool) (string, error) {
	return s3connector.NormalizeTransferPath(value, directory)
}
func (adapter) JoinTransferPath(directory, relative string) (string, error) {
	return s3connector.JoinTransferPath(directory, relative)
}
func (adapter) ParentTransferPath(directory string) string {
	return s3connector.ParentTransferPath(directory)
}
func (adapter) ValidateDownloadPaths(paths []string) error {
	return s3connector.ValidateDownloadPaths(paths)
}
