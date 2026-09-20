//go:build !windows

package execution

import "os"

func renameKnownHostsFile(sourcePath, targetPath string) error {
	return os.Rename(sourcePath, targetPath)
}

func syncKnownHostsDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
