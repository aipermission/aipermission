// Package keymaterial owns SQLCipher password text and raw-key syntax.
package keymaterial

import (
	"encoding/hex"
	"errors"
	"strings"
)

// ValidatePassphrase rejects the three raw-key forms recognized by the pinned
// SQLCipher runtime. Read/open paths intentionally retain existing-file support.
func ValidatePassphrase(value string) error {
	if len(value) != 67 && len(value) != 99 && len(value) != 163 {
		return nil
	}
	if (value[0] != 'x' && value[0] != 'X') || value[1] != '\'' || value[len(value)-1] != '\'' {
		return nil
	}
	if _, err := hex.DecodeString(value[2 : len(value)-1]); err != nil {
		return nil
	}
	return errors.New("database password must be a passphrase, not SQLCipher raw-key notation")
}

// EscapeDoubleQuoted preserves password text inside SQLCipher PRAGMA strings.
func EscapeDoubleQuoted(value string) string { return strings.ReplaceAll(value, `"`, `""`) }
