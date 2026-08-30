package api

import (
	"path"
	"strings"
	"testing"
	"unicode"
)

func FuzzTransferPathNormalization(f *testing.F) {
	f.Add(byte(0), "/home/developer/report.csv")
	f.Add(byte(1), "/var/lib/app/../app/data")
	f.Add(byte(2), `reports\2026\daily.csv`)
	f.Add(byte(2), "../../etc/passwd")
	f.Add(byte(0), "relative/file")

	f.Fuzz(func(t *testing.T, kind byte, input string) {
		if len(input) > 16<<10 {
			return
		}
		var (
			normalized string
			err        error
		)
		switch kind % 3 {
		case 0:
			normalized, err = normalizeRemoteFilePath(input)
		case 1:
			normalized, err = normalizeRemoteDirectoryPath(input)
		default:
			normalized, err = normalizeRelativeTransferPath(input)
		}
		if err != nil {
			return
		}
		if normalized == "" || normalized != path.Clean(normalized) {
			t.Fatalf("accepted path is not canonical: %q", normalized)
		}
		for _, character := range normalized {
			if unicode.IsControl(character) {
				t.Fatalf("accepted path contains control character: %q", normalized)
			}
		}
		if kind%3 == 2 {
			if path.IsAbs(normalized) || normalized == ".." || strings.HasPrefix(normalized, "../") || strings.Contains(normalized, `\`) {
				t.Fatalf("accepted relative path escapes its root: %q", normalized)
			}
		} else if !path.IsAbs(normalized) {
			t.Fatalf("accepted remote path is not absolute: %q", normalized)
		}
	})
}
