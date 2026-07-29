package projectvault

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	itemEncryptionVersion = 1
	itemAADSchemaVersion  = 1
	itemAADDomain         = "project-vault-item"
)

func itemAssociatedData(workspaceUUID string, itemID int64, valueVersion int64, encryptionVersion int) ([]byte, error) {
	if workspaceUUID == "" {
		return nil, fmt.Errorf("workspace UUID is required")
	}
	if itemID < 1 || valueVersion < 1 || encryptionVersion != itemEncryptionVersion {
		return nil, fmt.Errorf("invalid vault item encryption context")
	}

	var output bytes.Buffer
	output.WriteByte(itemAADSchemaVersion)
	writeAADString(&output, itemAADDomain)
	writeAADString(&output, workspaceUUID)
	_ = binary.Write(&output, binary.BigEndian, uint64(itemID))
	_ = binary.Write(&output, binary.BigEndian, uint64(valueVersion))
	_ = binary.Write(&output, binary.BigEndian, uint32(encryptionVersion))
	return output.Bytes(), nil
}

func writeAADString(output *bytes.Buffer, value string) {
	_ = binary.Write(output, binary.BigEndian, uint32(len(value)))
	output.WriteString(value)
}
