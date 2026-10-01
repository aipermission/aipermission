package keycleanup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func testDigest(t *testing.T, value string) string {
	t.Helper()
	digest, err := Digest(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func testFingerprint(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
}

func testIdentity(t *testing.T) Identity {
	t.Helper()
	identity, err := NewIdentity(Identity{
		TargetID: 1, TargetRevision: "target-v1", ConfigDigest: testDigest(t, "config"),
		Host: "example.test", Port: 22, Username: "operator", KeyDigest: testDigest(t, "public-key"),
		HostFingerprints: []string{testFingerprint("host")},
		Profiles:         []ProfileIdentity{{ID: 2, Revision: "profile-v1", SecretRevision: "1", PublicDigest: testDigest(t, "public"), KeyID: 3, KeyRevision: "key-v1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

type memoryStore struct {
	resourcecontract.CredentialResourceStore
	rows        map[int64]resourcecontract.CredentialResource
	nextID      int64
	listErr     error
	getErr      error
	createErr   error
	updateErr   error
	createAfter bool
	updateAfter bool
	readback    func(resourcecontract.CredentialResource) resourcecontract.CredentialResource
	secretReads int
	listOrder   []int64
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rows: map[int64]resourcecontract.CredentialResource{}, nextID: 1}
}

func (store *memoryStore) List(context.Context) ([]resourcecontract.CredentialResource, error) {
	if store.listErr != nil {
		return nil, store.listErr
	}
	rows := []resourcecontract.CredentialResource{}
	if len(store.listOrder) > 0 {
		for _, id := range store.listOrder {
			rows = append(rows, store.rows[id])
		}
		return rows, nil
	}
	for _, row := range store.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (store *memoryStore) Get(_ context.Context, id int64) (resourcecontract.CredentialResource, error) {
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
	store.secretReads++
	return errors.New("unexpected journal secret read")
}

func (store *memoryStore) Create(_ context.Context, input resourcecontract.CreateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if store.createErr != nil && !store.createAfter {
		return resourcecontract.CredentialResource{}, store.createErr
	}
	for _, row := range store.rows {
		if row.Name == input.Name {
			return resourcecontract.CredentialResource{}, resourcecontract.ErrCredentialResourceNameExists
		}
	}
	row := resourcecontract.CredentialResource{ID: store.nextID, Name: input.Name, ResourceType: input.ResourceType, PublicData: input.PublicData, Fingerprint: input.Fingerprint}
	store.nextID++
	store.rows[row.ID] = row
	if store.createErr != nil {
		return resourcecontract.CredentialResource{}, store.createErr
	}
	if store.readback != nil {
		row = store.readback(row)
	}
	return row, nil
}

func (store *memoryStore) Update(_ context.Context, id int64, input resourcecontract.UpdateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if store.updateErr != nil && !store.updateAfter {
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
	if store.readback != nil {
		row = store.readback(row)
	}
	return row, nil
}

func beginTest(t *testing.T, journal *Journal, identity Identity) Entry {
	t.Helper()
	entry, dispatch, err := journal.Begin(context.Background(), identity)
	if err != nil || !dispatch || entry.Record.Status != Intent {
		t.Fatalf("begin = %#v, dispatch %t, %v", entry, dispatch, err)
	}
	return entry
}

func seedTestEntry(t *testing.T, store *memoryStore, record Record) Entry {
	t.Helper()
	var err error
	record.Version = recordVersion
	record.Generation, err = newGeneration()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(record.Identity)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := store.Create(context.Background(), resourcecontract.CreateCredentialResourceInput{
		Name: resourceName(digest), ResourceType: recordType, PublicData: string(encoded), Fingerprint: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	return Entry{ResourceID: resource.ID, Record: record}
}
