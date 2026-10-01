package rolejournal

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func testAnchor() Anchor {
	return Anchor{
		TargetID: 1, ContextDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TargetDigest:   "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		AdminProfileID: 2, ClusterID: "18446744073709551615", DatabaseOID: 12, DatabaseName: " Main DB ",
		SuccessorOID: 10, SuccessorName: " Main Admin ",
	}
}

type memoryStore struct {
	resourcecontract.CredentialResourceStore
	rows      map[int64]resourcecontract.CredentialResource
	nextID    int64
	createErr error
	updateErr error
	listErr   error
	getErr    error
	after     bool
	transform func(resourcecontract.CredentialResource) resourcecontract.CredentialResource
	creates   int
	updates   int
	reads     int
	listed    []resourcecontract.CredentialResource
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rows: map[int64]resourcecontract.CredentialResource{}, nextID: 1}
}

func (store *memoryStore) List(ctx context.Context) ([]resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if store.listErr != nil {
		return nil, store.listErr
	}
	if store.listed != nil {
		return store.listed, nil
	}
	rows := make([]resourcecontract.CredentialResource, 0, len(store.rows))
	for _, row := range store.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (store *memoryStore) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	if store.getErr != nil {
		return resourcecontract.CredentialResource{}, store.getErr
	}
	row, found := store.rows[id]
	if !found {
		return resourcecontract.CredentialResource{}, resourcecontract.ErrCredentialResourceNotFound
	}
	return row, nil
}

func (store *memoryStore) GetSecret(context.Context, int64, any) error {
	store.reads++
	return errors.New("unexpected journal secret read")
}

func (store *memoryStore) Create(ctx context.Context, input resourcecontract.CreateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	store.creates++
	if store.createErr != nil && !store.after {
		return resourcecontract.CredentialResource{}, store.createErr
	}
	for _, row := range store.rows {
		if row.Name == input.Name {
			return resourcecontract.CredentialResource{}, resourcecontract.ErrCredentialResourceNameExists
		}
	}
	row := resourcecontract.CredentialResource{ID: store.nextID, Name: input.Name, ResourceType: input.ResourceType,
		PublicData: input.PublicData, Fingerprint: input.Fingerprint}
	store.nextID++
	store.rows[row.ID] = row
	if store.createErr != nil {
		return resourcecontract.CredentialResource{}, store.createErr
	}
	if store.transform != nil {
		row = store.transform(row)
	}
	return row, nil
}

func (store *memoryStore) Update(ctx context.Context, id int64, input resourcecontract.UpdateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	store.updates++
	if store.updateErr != nil && !store.after {
		return resourcecontract.CredentialResource{}, store.updateErr
	}
	row, found := store.rows[id]
	if !found {
		return resourcecontract.CredentialResource{}, resourcecontract.ErrCredentialResourceNotFound
	}
	row.Name, row.PublicData = input.Name, input.PublicData
	store.rows[id] = row
	if store.updateErr != nil {
		return resourcecontract.CredentialResource{}, store.updateErr
	}
	if store.transform != nil {
		row = store.transform(row)
	}
	return row, nil
}

func provisionTest(t *testing.T, journal *Journal) Entry {
	t.Helper()
	entry, err := journal.BeginProvision(t.Context(), testAnchor(), " My Role ")
	if err != nil {
		t.Fatal(err)
	}
	entry, err = journal.BindRole(t.Context(), entry, 42)
	if err != nil {
		t.Fatal(err)
	}
	entry, err = journal.ConfirmProvision(t.Context(), entry)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func resourceTest(t *testing.T) resourcecontract.CredentialResource {
	t.Helper()
	intent := Intent{Anchor: testAnchor(), RoleName: " My Role ", OperationID: "abcdef0123456789abcdef0123456789"}
	record := Record{Version: 1, Intent: intent, Generation: "1234567890abcdef1234567890abcdef", RoleOID: 42, Status: Provisioned}
	return encodeTestRecord(t, resourcecontract.CredentialResource{ID: 1, Name: resourceName(intent),
		ResourceType: recordType, Fingerprint: intentDigest(intent)}, record)
}

func encodeTestRecord(t *testing.T, resource resourcecontract.CredentialResource, record Record) resourcecontract.CredentialResource {
	t.Helper()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	resource.PublicData = string(encoded)
	return resource
}
