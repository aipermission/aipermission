package rolejournal

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func readEntry(ctx context.Context, reader resourcecontract.CredentialResourceReader, id int64) (Entry, error) {
	if ctx == nil || resourcecontract.IsNilDependency(reader) || id < 1 {
		return Entry{}, errors.New("managed Postgres role journal or record is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	resource, err := reader.Get(ctx, id)
	if err != nil {
		return Entry{}, fmt.Errorf("read managed Postgres role generation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	entry, err := parseResource(resource)
	if err == nil && entry.ResourceID != id {
		return Entry{}, errors.New("managed Postgres role read returned another record")
	}
	return entry, err
}

func resolveReference(ctx context.Context, reference Reference, get func(context.Context, int64) (Entry, error)) (Entry, error) {
	id, err := reference.resourceID()
	if err != nil {
		return Entry{}, err
	}
	entry, err := get(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	if entry.Reference() != reference {
		return Entry{}, errors.New("managed Postgres role reference does not match the durable identity")
	}
	return entry, nil
}

func readCurrent(ctx context.Context, expected Entry, get func(context.Context, int64) (Entry, error)) (Entry, error) {
	if err := expected.Record.Validate(); err != nil {
		return Entry{}, err
	}
	current, err := get(ctx, expected.ResourceID)
	if err != nil {
		return Entry{}, err
	}
	if !reflect.DeepEqual(current, expected) {
		return Entry{}, ErrStaleGeneration
	}
	return current, nil
}
