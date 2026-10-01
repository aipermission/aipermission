package rolejournal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
)

// Reference is immutable credential metadata. Generation and lifecycle status
// live only in the journal, so confirming cleanup does not invalidate a retry.
type Reference struct {
	ResourceID string `json:"resource_id"`
	Intent     Intent `json:"intent"`
	RoleOID    uint32 `json:"role_oid"`
}

func (entry Entry) Reference() Reference {
	return Reference{ResourceID: strconv.FormatInt(entry.ResourceID, 10), Intent: entry.Record.Intent, RoleOID: entry.Record.RoleOID}
}

func ParseReference(value any) (Reference, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxRecordBytes {
		return Reference{}, errors.New("invalid managed Postgres role reference")
	}
	var ref Reference
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ref); err != nil {
		return Reference{}, errors.New("invalid managed Postgres role reference")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Reference{}, errors.New("invalid trailing managed Postgres role reference")
	}
	if _, err := ref.resourceID(); err != nil {
		return Reference{}, err
	}
	return ref, nil
}

func (reference Reference) resourceID() (int64, error) {
	id, err := strconv.ParseInt(reference.ResourceID, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != reference.ResourceID ||
		reference.RoleOID == 0 || reference.RoleOID == reference.Intent.Anchor.SuccessorOID {
		return 0, errors.New("invalid managed Postgres role reference identity")
	}
	if err := reference.Intent.validate(); err != nil {
		return 0, err
	}
	return id, nil
}

func (journal *Journal) ResolveReference(ctx context.Context, reference Reference) (Entry, error) {
	return resolveReference(ctx, reference, journal.Get)
}
