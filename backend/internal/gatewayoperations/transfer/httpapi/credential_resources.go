package filetransferhttp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

var errTransferCredentialResourcesReadOnly = errors.New("file transfer credential resources are read-only")

type credentialResourceKey struct {
	kind string
	id   int64
}

type credentialResourceBindings struct {
	mu       sync.Mutex
	records  map[credentialResourceKey]connectorapi.CredentialResource
	boundary actionresult.CredentialBoundary
}

func newCredentialResourceBindings(boundary actionresult.CredentialBoundary) *credentialResourceBindings {
	return &credentialResourceBindings{
		records:  make(map[credentialResourceKey]connectorapi.CredentialResource),
		boundary: boundary,
	}
}

func (b *credentialResourceBindings) bind(kind string, record connectorapi.CredentialResource) error {
	if b == nil {
		return errTransferExecutionStale
	}
	key := credentialResourceKey{kind: kind, id: record.ID}
	b.mu.Lock()
	defer b.mu.Unlock()
	if accepted, ok := b.records[key]; ok && !reflect.DeepEqual(accepted, record) {
		return errTransferExecutionStale
	}
	b.records[key] = record
	return nil
}

func (b *credentialResourceBindings) addSecret(destination any) {
	if b == nil || !b.boundary.Valid() {
		return
	}
	raw, err := json.Marshal(destination)
	if err != nil {
		return
	}
	var structured any
	if err := json.Unmarshal(raw, &structured); err == nil {
		b.boundary.AddStructured(structured)
	}
}

type boundCredentialResourceStore struct {
	delegate connectorapi.CredentialResourceStore
	kind     string
	bindings *credentialResourceBindings
}

func (s boundCredentialResourceStore) List(ctx context.Context) ([]connectorapi.CredentialResource, error) {
	records, err := s.delegate.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if err := s.bindings.bind(s.kind, record); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s boundCredentialResourceStore) Get(ctx context.Context, id int64) (connectorapi.CredentialResource, error) {
	record, err := s.delegate.Get(ctx, id)
	if err != nil {
		return connectorapi.CredentialResource{}, err
	}
	if err := s.bindings.bind(s.kind, record); err != nil {
		return connectorapi.CredentialResource{}, err
	}
	return record, nil
}

func (s boundCredentialResourceStore) GetSecret(ctx context.Context, id int64, destination any) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	err := s.delegate.GetSecret(ctx, id, destination)
	s.bindings.addSecret(destination)
	if err != nil {
		return err
	}
	_, err = s.Get(ctx, id)
	return err
}

func (boundCredentialResourceStore) Create(context.Context, connectorapi.CreateCredentialResourceInput) (connectorapi.CredentialResource, error) {
	return connectorapi.CredentialResource{}, errTransferCredentialResourcesReadOnly
}

func (boundCredentialResourceStore) Update(context.Context, int64, connectorapi.UpdateCredentialResourceInput) (connectorapi.CredentialResource, error) {
	return connectorapi.CredentialResource{}, errTransferCredentialResourcesReadOnly
}

func (boundCredentialResourceStore) Delete(context.Context, int64) error {
	return errTransferCredentialResourcesReadOnly
}

func (s boundCredentialResourceStore) CountProfileReferences(ctx context.Context, publicField string, numericValue int64) (int, error) {
	return s.delegate.CountProfileReferences(ctx, publicField, numericValue)
}

var _ connectorapi.CredentialResourceStore = boundCredentialResourceStore{}
