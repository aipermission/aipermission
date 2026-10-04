package remotemetadata

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/sftp"
)

// Metadata retains exact unsigned ownership IDs and full file-mode attributes.
type Metadata struct {
	Mode os.FileMode
	UID  uint32
	GID  uint32
}

func parse(output string) (Metadata, error) {
	fields := strings.Fields(output)
	if len(fields) != 4 {
		return Metadata{}, fmt.Errorf("read complete remote metadata: unexpected stat response")
	}
	modeBase := 0
	switch fields[0] {
	case "gnu":
		modeBase = 16
	case "bsd":
		modeBase = 8
	default:
		return Metadata{}, fmt.Errorf("read complete remote metadata: unknown stat response")
	}
	rawMode, err := strconv.ParseUint(fields[1], modeBase, 32)
	if err != nil {
		return Metadata{}, fmt.Errorf("read complete remote metadata mode: %w", err)
	}
	uid, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return Metadata{}, fmt.Errorf("read complete remote metadata owner: %w", err)
	}
	gid, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil {
		return Metadata{}, fmt.Errorf("read complete remote metadata group: %w", err)
	}
	mode := (&sftp.FileStat{Mode: uint32(rawMode)}).FileMode()
	return Metadata{Mode: mode, UID: uint32(uid), GID: uint32(gid)}, nil
}

func command(remotePath string) string {
	if strings.HasPrefix(remotePath, "-") {
		remotePath = "./" + remotePath
	}
	quotedPath := quoteShellArg(remotePath)
	return "if aip_stat=$(LC_ALL=C stat -c '%f %u %g' " + quotedPath + " 2>/dev/null); then " +
		"printf 'gnu %s\\n' \"$aip_stat\"; " +
		"elif aip_stat=$(LC_ALL=C stat -f '%p %u %g' " + quotedPath + " 2>/dev/null); then " +
		"printf 'bsd %s\\n' \"$aip_stat\"; else exit 1; fi"
}

func quoteShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
