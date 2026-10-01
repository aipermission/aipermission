package rolejournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

// Callers hold workspace lifecycle exclusion across snapshot, intent, dispatch
// and confirmation. The scoped store is durable but does not provide a CAS.
type Journal struct {
	store resourcecontract.CredentialResourceStore
}

func New(store resourcecontract.CredentialResourceStore) *Journal {
	if resourcecontract.IsNilDependency(store) {
		store = nil
	}
	return &Journal{store: store}
}

func (journal *Journal) List(ctx context.Context) ([]Entry, error) {
	if journal == nil || journal.store == nil {
		return nil, errors.New("managed Postgres role journal is unavailable")
	}
	resources, err := journal.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read managed Postgres role journal: %w", err)
	}
	entries := make([]Entry, 0, len(resources))
	seenIDs, seenNames := map[int64]bool{}, map[string]bool{}
	for _, resource := range resources {
		entry, err := parseResource(resource)
		if err != nil {
			return nil, err
		}
		if seenIDs[resource.ID] || seenNames[resource.Name] {
			return nil, errors.New("duplicate managed Postgres role journal identity")
		}
		seenIDs[resource.ID], seenNames[resource.Name] = true, true
		entries = append(entries, entry)
	}
	return entries, nil
}

func (journal *Journal) Get(ctx context.Context, id int64) (Entry, error) {
	if journal == nil || journal.store == nil || id < 1 {
		return Entry{}, errors.New("managed Postgres role journal or record is unavailable")
	}
	return readEntry(ctx, journal.store, id)
}

// BeginProvision persists uncertainty before any role mutation. An unresolved
// predecessor fences endpoint drift as well as aliases to the same cluster.
func (journal *Journal) BeginProvision(ctx context.Context, anchor Anchor, roleName string) (Entry, error) {
	operation, err := randomID()
	if err != nil {
		return Entry{}, err
	}
	intent := Intent{Anchor: anchor, RoleName: roleName, OperationID: operation}
	if err := intent.validate(); err != nil {
		return Entry{}, err
	}
	entries, err := journal.List(ctx)
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if entry.Record.Intent.overlaps(intent) && entry.Record.Status != Cleaned && entry.Record.Status != RolledBack {
			return Entry{}, reconciliationError(entry)
		}
	}
	generation, err := randomID()
	if err != nil {
		return Entry{}, err
	}
	record := Record{Version: 1, Intent: intent, Generation: generation, Status: ProvisionIntent}
	encoded, err := json.Marshal(record)
	if err != nil {
		return Entry{}, err
	}
	resource, err := journal.store.Create(ctx, resourcecontract.CreateCredentialResourceInput{
		Name: resourceName(intent), ResourceType: recordType, PublicData: string(encoded), Fingerprint: intentDigest(intent), Secret: struct{}{},
	})
	if err != nil {
		return Entry{}, fmt.Errorf("persist managed Postgres role intent: %w", err)
	}
	return journal.readback(ctx, resource, record)
}

func (journal *Journal) readback(ctx context.Context, resource resourcecontract.CredentialResource, expected Record) (Entry, error) {
	entry, err := parseResource(resource)
	if err != nil || !reflect.DeepEqual(entry.Record, expected) {
		return Entry{}, errors.New("managed Postgres role persistence is unconfirmed")
	}
	fresh, err := journal.Get(ctx, entry.ResourceID)
	if err != nil {
		return Entry{}, err
	}
	if !reflect.DeepEqual(fresh, entry) {
		return Entry{}, errors.New("managed Postgres role persistence readback changed")
	}
	return fresh, nil
}

func reconciliationError(entry Entry) error {
	return fmt.Errorf("%w (record %d, generation %s, status %s)", ErrReconciliationRequired,
		entry.ResourceID, entry.Record.Generation, entry.Record.Status)
}
