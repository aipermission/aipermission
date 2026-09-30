package keycleanup

import (
	"context"
	"errors"
)

// ListForTarget retains retired groups and historical target aliases. Current
// profile membership alone cannot identify every unfinished remote revocation.
func (journal *Journal) ListForTarget(ctx context.Context, targetID int64) ([]Entry, error) {
	if targetID < 1 {
		return nil, errors.New("SSH key revocation target identity is invalid")
	}
	entries, err := journal.List(ctx)
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, entry := range entries {
		if entry.Record.referencesTarget(targetID) {
			result = append(result, entry)
		}
	}
	return result, nil
}

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
