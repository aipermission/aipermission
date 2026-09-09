package connectors

import (
	"fmt"
	"strconv"
	"strings"
)

const targetRefSeparator = ":"

// FormatTargetRef returns the canonical identity for a target/profile pair.
func FormatTargetRef(connectorKind string, targetID, profileID int64) string {
	return fmt.Sprintf("%s%s%d%s%d", connectorKind, targetRefSeparator, targetID, targetRefSeparator, profileID)
}

// ParseTargetRef validates and splits a canonical target/profile identity.
func ParseTargetRef(ref string) (string, int64, int64, bool) {
	parts := strings.Split(strings.TrimSpace(ref), targetRefSeparator)
	if len(parts) != 3 || !ValidIdentifier(parts[0]) {
		return "", 0, 0, false
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || targetID < 1 {
		return "", 0, 0, false
	}
	profileID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || profileID < 1 {
		return "", 0, 0, false
	}
	return parts[0], targetID, profileID, true
}
