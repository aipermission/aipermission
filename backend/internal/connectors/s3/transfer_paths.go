package s3connector

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Transfer locators add exactly one slash to the opaque bucket key.
func NormalizeTransferPath(value string, directory bool) (string, error) {
	if directory && value == "" {
		value = "/"
	}
	if !strings.HasPrefix(value, "/") || !utf8.ValidString(value) || len(value) > 1025 {
		return "", fmt.Errorf("S3 transfer path must be / followed by an exact UTF-8 key of at most 1024 bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("S3 transfer paths cannot contain control characters")
		}
	}
	if !directory && strings.HasSuffix(value, "/") {
		return "", fmt.Errorf("S3 transfer files cannot have empty or trailing-slash keys")
	}
	return value, nil
}

func JoinTransferPath(directory, relative string) (string, error) {
	directory, err := NormalizeTransferPath(directory, true)
	if err != nil {
		return "", err
	}
	if relative == "" {
		return "", fmt.Errorf("S3 upload filename is required")
	}
	if !strings.HasSuffix(directory, "/") {
		directory += "/"
	}
	return NormalizeTransferPath(directory+relative, false)
}

func ParentTransferPath(directory string) string {
	key := strings.TrimSuffix(strings.TrimPrefix(directory, "/"), "/")
	if index := strings.LastIndex(key, "/"); index >= 0 {
		return "/" + key[:index+1]
	}
	return "/"
}

// ZIP hierarchy cannot represent these components without renaming identities.
// Single downloads have a separately reported safe local filename.
func ValidateDownloadPaths(paths []string) error {
	for _, locator := range paths {
		for _, part := range strings.Split(strings.TrimPrefix(locator, "/"), "/") {
			if part == "" || strings.HasPrefix(part, ".") || len([]rune(part)) > 160 || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") || strings.ContainsAny(part, "\\:<>\"|?*\x00") {
				return fmt.Errorf("S3 keys cannot be mapped losslessly into this ZIP hierarchy; download these objects individually")
			}
		}
	}
	return nil
}
