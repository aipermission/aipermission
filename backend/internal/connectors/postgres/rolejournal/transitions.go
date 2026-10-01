package rolejournal

import (
	"context"
	"encoding/json"
	"errors"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

// BindRole persists the transaction's exact role OID before remote COMMIT.
func (journal *Journal) BindRole(ctx context.Context, expected Entry, oid uint32) (Entry, error) {
	return journal.transition(ctx, expected, ProvisionIntent, func(record *Record) error {
		if record.RoleOID != 0 || oid == 0 || oid == record.Intent.Anchor.SuccessorOID {
			return errors.New("managed Postgres role identity cannot be rebound")
		}
		record.RoleOID = oid
		return nil
	})
}

// ConfirmProvision requires a COMMIT acknowledgement or exact remote identity
// verification under the connector's catalog fence, never just name existence.
func (journal *Journal) ConfirmProvision(ctx context.Context, expected Entry) (Entry, error) {
	return journal.transition(ctx, expected, ProvisionIntent, func(record *Record) error {
		record.Status = Provisioned
		return nil
	})
}

// ConfirmRollback requires a successful remote rollback, not connection loss or
// observing an absent role name. An uncertain transaction retains its intent.
func (journal *Journal) ConfirmRollback(ctx context.Context, expected Entry) (Entry, error) {
	return journal.transition(ctx, expected, ProvisionIntent, func(record *Record) error {
		record.Status = RolledBack
		return nil
	})
}

// BeginCleanup skips remote dispatch only for a fresh terminal confirmation.
func (journal *Journal) BeginCleanup(ctx context.Context, expected Entry) (Entry, bool, error) {
	current, err := journal.current(ctx, expected)
	if err != nil {
		return Entry{}, false, err
	}
	if current.Record.Status == Cleaned {
		return current, false, nil
	}
	if current.Record.Status != Provisioned {
		return Entry{}, false, reconciliationError(current)
	}
	next, err := journal.transition(ctx, current, Provisioned, func(record *Record) error {
		record.Status = CleanupIntent
		return nil
	})
	return next, err == nil, err
}

// ConfirmCleanup requires an acknowledged transaction that reassigned ownership,
// revoked privileges and dropped the bound role. Absence alone is not evidence.
func (journal *Journal) ConfirmCleanup(ctx context.Context, expected Entry) (Entry, error) {
	return journal.transition(ctx, expected, CleanupIntent, func(record *Record) error {
		record.Status = Cleaned
		return nil
	})
}

// ConfirmCleanupRollback requires acknowledged rollback of the entire cleanup
// transaction. The role remains provisioned; no privileges or ownership changes
// are claimed and a later explicitly initiated deletion can retry safely.
func (journal *Journal) ConfirmCleanupRollback(ctx context.Context, expected Entry) (Entry, error) {
	return journal.restoreProvisioned(ctx, expected)
}

// ConfirmCleanupNotApplied requires a fresh catalog fence and verification of
// the entire bound remote identity. The atomic cleanup would have dropped this
// role if committed; an absent name never authorizes this transition. This is
// not an acknowledgement of the original transaction's rollback.
func (journal *Journal) ConfirmCleanupNotApplied(ctx context.Context, expected Entry) (Entry, error) {
	return journal.restoreProvisioned(ctx, expected)
}

func (journal *Journal) restoreProvisioned(ctx context.Context, expected Entry) (Entry, error) {
	return journal.transition(ctx, expected, CleanupIntent, func(record *Record) error {
		record.Status = Provisioned
		return nil
	})
}

// ValidateCurrent rejects stale operator snapshots before opening a remote
// connection. Workspace lifecycle exclusion must span this check and decision.
func (journal *Journal) ValidateCurrent(ctx context.Context, expected Entry) error {
	_, err := journal.current(ctx, expected)
	return err
}

func (journal *Journal) current(ctx context.Context, expected Entry) (Entry, error) {
	return readCurrent(ctx, expected, journal.Get)
}

func (journal *Journal) transition(ctx context.Context, expected Entry, from Status, mutate func(*Record) error) (Entry, error) {
	current, err := journal.current(ctx, expected)
	if err != nil {
		return Entry{}, err
	}
	if current.Record.Status != from {
		return Entry{}, reconciliationError(current)
	}
	next := current.Record
	if err := mutate(&next); err != nil {
		return Entry{}, err
	}
	next.Generation, err = randomID()
	if err != nil {
		return Entry{}, err
	}
	if err := next.Validate(); err != nil {
		return Entry{}, err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return Entry{}, err
	}
	resource, err := journal.store.Update(ctx, current.ResourceID, resourcecontract.UpdateCredentialResourceInput{
		Name: resourceName(next.Intent), PublicData: string(encoded),
	})
	if err != nil {
		return Entry{}, err
	}
	if resource.ID != current.ResourceID {
		return Entry{}, errors.New("managed Postgres role persistence returned another record")
	}
	return journal.readback(ctx, resource, next)
}
