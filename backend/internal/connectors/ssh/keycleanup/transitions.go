package keycleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (journal *Journal) Confirm(ctx context.Context, expected Entry) (Entry, error) {
	return journal.transition(ctx, expected, func(record *Record) error {
		if record.Status != Intent {
			return ErrStaleGeneration
		}
		record.Status = Confirmed
		return nil
	})
}

// Attest records explicit external absence evidence for every historical and
// selected location. The caller must recompute/bind the current deletion context
// under lifecycle exclusion. This never authenticates or erases earlier proof.
func (journal *Journal) Attest(ctx context.Context, expected Entry, decision Attestation) (Entry, error) {
	if err := expected.Record.validate(); err != nil {
		return Entry{}, err
	}
	if err := decision.Identity.validate(); err != nil {
		return Entry{}, err
	}
	if !expected.Record.overlaps(decision.Identity) {
		return Entry{}, errors.New("SSH key revocation attestation identity does not match")
	}
	decision.Reason = strings.TrimSpace(decision.Reason)
	if err := validateReason(decision.Reason); err != nil {
		return Entry{}, err
	}
	if err := validateEvidence(expected.Record, decision); err != nil {
		return Entry{}, err
	}
	return journal.transition(ctx, expected, func(record *Record) error {
		record.Status = Attested
		record.Attestations = append(record.Attestations, decision)
		return nil
	})
}

func (journal *Journal) transition(ctx context.Context, expected Entry, mutate func(*Record) error) (Entry, error) {
	if journal == nil || journal.store == nil {
		return Entry{}, errors.New("SSH key revocation journal is unavailable")
	}
	resource, err := journal.store.Get(ctx, expected.ResourceID)
	if err != nil {
		return Entry{}, fmt.Errorf("read SSH key revocation generation: %w", err)
	}
	current, err := parseResource(resource)
	if err != nil {
		return Entry{}, err
	}
	if !reflect.DeepEqual(current, expected) {
		return Entry{}, ErrStaleGeneration
	}
	next := current.Record
	if err := mutate(&next); err != nil {
		return Entry{}, err
	}
	next.Generation, err = newGeneration()
	if err != nil {
		return Entry{}, err
	}
	if err := next.validate(); err != nil {
		return Entry{}, err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return Entry{}, err
	}
	resource, err = journal.store.Update(ctx, current.ResourceID, connectorapi.UpdateCredentialResourceInput{
		Name: resource.Name, PublicData: string(encoded),
	})
	if err != nil {
		return Entry{}, fmt.Errorf("persist SSH key revocation decision: %w", err)
	}
	updated, err := parseResource(resource)
	if err != nil || updated.ResourceID != current.ResourceID || !reflect.DeepEqual(updated.Record, next) {
		return Entry{}, errors.New("SSH key revocation decision persistence is unconfirmed")
	}
	return updated, nil
}
