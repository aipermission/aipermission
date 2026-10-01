package rolecatalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/jackc/pgx/v5/pgconn"
)

// This fake exercises real journal parsing, readback and transitions, not a
// substitute lifecycle state machine. SQLCipher and real SQL have separate tests.
type lifecycleStore struct {
	resourcecontract.CredentialResourceStore
	row          resourcecontract.CredentialResource
	last         rolejournal.Record
	createErr    error
	failStatus   rolejournal.Status
	failBinding  bool
	afterWrite   bool
	beforeUpdate func(rolejournal.Record)
}

func (store *lifecycleStore) List(ctx context.Context) ([]resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if store.row.ID == 0 {
		return nil, nil
	}
	return []resourcecontract.CredentialResource{store.row}, nil
}

func (store *lifecycleStore) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	if store.row.ID != id {
		return resourcecontract.CredentialResource{}, resourcecontract.ErrCredentialResourceNotFound
	}
	return store.row, nil
}

func (store *lifecycleStore) Create(ctx context.Context, input resourcecontract.CreateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	if store.createErr != nil {
		return resourcecontract.CredentialResource{}, store.createErr
	}
	store.row = resourcecontract.CredentialResource{ID: 1, Name: input.Name, ResourceType: input.ResourceType,
		PublicData: input.PublicData, Fingerprint: input.Fingerprint}
	if err := json.Unmarshal([]byte(input.PublicData), &store.last); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	return store.row, nil
}

func (store *lifecycleStore) Update(ctx context.Context, id int64, input resourcecontract.UpdateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	var next rolejournal.Record
	if err := json.Unmarshal([]byte(input.PublicData), &next); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	if store.beforeUpdate != nil {
		store.beforeUpdate(next)
	}
	fail := next.Status == store.failStatus || (store.failBinding && next.RoleOID != 0 && next.Status == rolejournal.ProvisionIntent)
	if fail && !store.afterWrite {
		return resourcecontract.CredentialResource{}, errors.New("journal update failed")
	}
	store.row.Name, store.row.PublicData, store.last = input.Name, input.PublicData, next
	if fail {
		return resourcecontract.CredentialResource{}, errors.New("journal update acknowledgement lost")
	}
	return store.row, nil
}

type lifecycleTx struct {
	*transaction
	execErr      error
	commitErr    error
	commits      int
	beforeExec   func(string)
	beforeCommit func()
}

func (tx *lifecycleTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if tx.beforeExec != nil {
		tx.beforeExec(sql)
	}
	if tx.execErr != nil && sql != catalogLock && !strings.HasPrefix(sql, "SELECT") && !strings.HasPrefix(sql, "SET LOCAL") {
		tx.statements = append(tx.statements, sql)
		return pgconn.CommandTag{}, tx.execErr
	}
	return tx.transaction.Exec(ctx, sql, arguments...)
}

func (tx *lifecycleTx) Commit(context.Context) error {
	tx.commits++
	if tx.beforeCommit != nil {
		tx.beforeCommit()
	}
	return tx.commitErr
}

type lifecycleConnection struct {
	*starter
	closes        int
	closeDeadline time.Duration
	closeCanceled bool
}

func (connection *lifecycleConnection) Close(ctx context.Context) error {
	connection.closes++
	connection.closeCanceled = ctx.Err() != nil
	if deadline, ok := ctx.Deadline(); ok {
		connection.closeDeadline = time.Until(deadline)
	}
	return nil
}

func lifecycleFixture(t *testing.T) (*rolejournal.Journal, *lifecycleStore, *lifecycleTx, *lifecycleConnection) {
	t.Helper()
	store := &lifecycleStore{}
	tx := &lifecycleTx{transaction: cleanupTransaction(t, [3]string{"table", "public", "fixture"})}
	tx.rows[roleQuery] = func(destination ...any) error {
		*destination[0].(*uint32) = 42
		*destination[1].(*string), *destination[2].(*string) = store.last.Intent.RoleName, store.last.Intent.Marker()
		return nil
	}
	connection := &lifecycleConnection{starter: &starter{tx: tx}}
	return rolejournal.New(store), store, tx, connection
}

func provisionFixture(t *testing.T, journal *rolejournal.Journal, connection *lifecycleConnection) rolejournal.Entry {
	t.Helper()
	entry, err := Provision(t.Context(), journal, testRecord().Intent.Anchor, " My Role ", []string{`CREATE ROLE " My Role "`},
		func(context.Context) (Connection, error) { return connection, nil })
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func mutationStatements(tx *lifecycleTx) []string {
	var statements []string
	for _, sql := range tx.statements {
		if sql != catalogLock && !strings.HasPrefix(sql, "SELECT") && !strings.HasPrefix(sql, "SET LOCAL") {
			statements = append(statements, sql)
		}
	}
	return statements
}
