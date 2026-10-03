package snapshotfile

import (
	"io"
	"os"
)

// Copy publishes a staged encrypted artifact to an exclusively owned candidate.
// It never overwrites an existing file and removes only its own failed copy.
func Copy(sourcePath string) func(string) error {
	return func(targetPath string) error {
		source, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		remove := true
		defer func() {
			_ = target.Close()
			if remove {
				_ = os.Remove(targetPath)
			}
		}()
		if _, err := io.Copy(target, source); err != nil {
			return err
		}
		if err := target.Sync(); err != nil {
			return err
		}
		if err := target.Close(); err != nil {
			return err
		}
		remove = false
		return nil
	}
}
