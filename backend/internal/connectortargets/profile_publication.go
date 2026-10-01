package connectortargets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Store) HasCredentialProfileLabel(ctx context.Context, targetID int64, label string) (bool, error) {
	profiles, err := s.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return false, fmt.Errorf("list connector credential profiles: %w", err)
	}
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Label), strings.TrimSpace(label)) {
			return true, nil
		}
	}
	return false, nil
}

// ConfirmCredentialProfilePublication requires fresh, exact persisted evidence.
// A missing row or failed read never proves that a transaction rolled back.
func (s *Store) ConfirmCredentialProfilePublication(ctx context.Context, expected CredentialProfile) (CredentialProfile, bool) {
	if expected.ID < 1 || expected.TargetID < 1 {
		return CredentialProfile{}, false
	}
	profile, err := s.GetCredentialProfile(ctx, expected.TargetID, expected.ID)
	if err != nil || !samePublishedProfile(profile, expected) {
		return CredentialProfile{}, false
	}
	return profile, true
}

func samePublishedProfile(actual, expected CredentialProfile) bool {
	if actual.ID != expected.ID || actual.TargetID != expected.TargetID || actual.ConnectorKind != expected.ConnectorKind ||
		actual.Kind != expected.Kind || actual.Label != expected.Label || actual.RiskLabel != expected.RiskLabel ||
		actual.EncryptedSecretJSON == "" || actual.EncryptedSecretJSON != expected.EncryptedSecretJSON ||
		actual.SecretRevision != expected.SecretRevision || actual.CreatedAt != expected.CreatedAt || actual.UpdatedAt != expected.UpdatedAt {
		return false
	}
	actualJSON, actualErr := json.Marshal(actual.Public)
	expectedJSON, expectedErr := json.Marshal(expected.Public)
	return actualErr == nil && expectedErr == nil && string(actualJSON) == string(expectedJSON)
}
