package rolejournal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
)

// ParseEntry validates the complete operator snapshot, not just an immutable
// profile reference. Resource IDs remain canonical decimal text on the wire.
func ParseEntry(value any) (Entry, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxRecordBytes {
		return Entry{}, errors.New("invalid managed Postgres role snapshot")
	}
	var wire struct {
		ResourceID string `json:"resource_id"`
		Record     Record `json:"record"`
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Entry{}, errors.New("invalid managed Postgres role snapshot data")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Entry{}, errors.New("invalid trailing managed Postgres role snapshot data")
	}
	id, err := strconv.ParseInt(wire.ResourceID, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != wire.ResourceID {
		return Entry{}, errors.New("invalid managed Postgres role snapshot resource identity")
	}
	if err := wire.Record.Validate(); err != nil {
		return Entry{}, err
	}
	return Entry{ResourceID: id, Record: wire.Record}, nil
}
