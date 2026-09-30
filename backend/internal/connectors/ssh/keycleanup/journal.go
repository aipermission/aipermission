package keycleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

// Callers must hold workspace lifecycle exclusion from snapshot through dispatch
// and confirmation. Resource-store updates alone do not provide a cross-step CAS.
type Journal struct {
	store connectorapi.CredentialResourceStore
}

func New(store connectorapi.CredentialResourceStore) *Journal { return &Journal{store: store} }

func (journal *Journal) List(ctx context.Context) ([]Entry, error) {
	if journal == nil || journal.store == nil {
		return nil, errors.New("SSH key revocation journal is unavailable")
	}
	resources, err := journal.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read SSH key revocation journal: %w", err)
	}
	entries := make([]Entry, 0, len(resources))
	seen := map[string]bool{}
	for _, resource := range resources {
		entry, err := parseResource(resource)
		if err != nil {
			return nil, err
		}
		if seen[resource.Name] {
			return nil, errors.New("duplicate SSH key revocation identity")
		}
		seen[resource.Name] = true
		entries = append(entries, entry)
	}
	return entries, nil
}

// Begin persists uncertainty before any private-key read or remote dispatch.
// dispatch is false only when all matching history is confirmed for this snapshot.
func (journal *Journal) Begin(ctx context.Context, identity Identity) (entry Entry, dispatch bool, err error) {
	if err := identity.validate(); err != nil {
		return Entry{}, false, err
	}
	entries, err := journal.List(ctx)
	if err != nil {
		return Entry{}, false, err
	}
	digest, err := Digest(identity)
	if err != nil {
		return Entry{}, false, err
	}
	completed, err := completedEntry(entries, identity, digest)
	if err != nil || completed != nil {
		if completed != nil {
			return *completed, false, err
		}
		return Entry{}, false, err
	}
	generation, err := newGeneration()
	if err != nil {
		return Entry{}, false, err
	}
	record := Record{Version: recordVersion, Identity: identity, Generation: generation, Status: Intent}
	encoded, err := json.Marshal(record)
	if err != nil {
		return Entry{}, false, err
	}
	resource, err := journal.store.Create(ctx, connectorapi.CreateCredentialResourceInput{
		Name: resourceName(digest), ResourceType: recordType, PublicData: string(encoded), Fingerprint: digest, Secret: struct{}{},
	})
	if err != nil {
		return Entry{}, false, fmt.Errorf("persist SSH key revocation intent: %w", err)
	}
	entry, err = parseResource(resource)
	if err != nil || !reflect.DeepEqual(entry.Record, record) {
		return Entry{}, false, errors.New("SSH key revocation intent persistence is unconfirmed")
	}
	return entry, true, nil
}

func completedEntry(entries []Entry, identity Identity, digest string) (*Entry, error) {
	var completed *Entry
	for _, entry := range entries {
		if !entry.Record.overlaps(identity) {
			continue
		}
		previous, err := Digest(entry.Record.Identity)
		if err != nil {
			return nil, err
		}
		exactConfirmation := previous == digest && entry.Record.Status == Confirmed
		exactAttestation := false
		if entry.Record.Status == Attested {
			latest := entry.Record.Attestations[len(entry.Record.Attestations)-1]
			verifiedDigest, err := Digest(latest.Identity)
			if err != nil {
				return nil, err
			}
			exactAttestation = verifiedDigest == digest
		}
		if !exactConfirmation && !exactAttestation {
			return nil, reconciliationError(entry)
		}
		copy := entry
		completed = &copy
	}
	return completed, nil
}
