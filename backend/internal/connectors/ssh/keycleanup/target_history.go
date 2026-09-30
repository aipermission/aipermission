package keycleanup

import (
	"context"
	"errors"
)

// ListForTarget retains retired groups and historical target aliases. Current
// profile membership alone cannot identify every unfinished remote revocation.
func (journal *Journal) ListForTarget(ctx context.Context, targetID int64, current ...Identity) ([]Entry, error) {
	if targetID < 1 {
		return nil, errors.New("SSH key revocation target identity is invalid")
	}
	for _, identity := range current {
		if err := identity.validate(); err != nil {
			return nil, err
		}
	}
	entries, err := journal.List(ctx)
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, entry := range entries {
		if entry.Record.referencesTarget(targetID) || entry.Record.overlapsAny(current) {
			result = append(result, entry)
		}
	}
	return result, nil
}

func (record Record) overlapsAny(identities []Identity) bool {
	for _, identity := range identities {
		if record.overlaps(identity) {
			return true
		}
	}
	return false
}

// MatchesIdentity uses the same historical alias rules as mutation admission.
// Consumers must obtain the record through the validating Journal reader.
func (record Record) MatchesIdentity(identity Identity) bool { return record.overlaps(identity) }

// RequireTargetHistoryResolved must precede all new intents and private-key
// delivery when deleting a target. Independent current groups still use Begin.
func (journal *Journal) RequireTargetHistoryResolved(ctx context.Context, targetID int64) error {
	entries, err := journal.ListForTarget(ctx, targetID)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Record.Status == Intent {
			return reconciliationError(entry)
		}
	}
	return nil
}

func (record Record) referencesTarget(targetID int64) bool {
	if record.Identity.TargetID == targetID {
		return true
	}
	for _, proof := range record.Attestations {
		if proof.Identity.TargetID == targetID {
			return true
		}
	}
	return false
}
