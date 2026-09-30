package keycleanup

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
)

type VerificationSubject struct {
	ID               string   `json:"id"`
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	Username         string   `json:"username"`
	KeyFingerprint   string   `json:"key_fingerprint"`
	HostFingerprints []string `json:"host_fingerprints"`
}

type AbsenceEvidence struct {
	SubjectID string `json:"subject_id"`
	Method    string `json:"method"`
	Absent    bool   `json:"absent"`
	Reason    string `json:"reason"`
}

// VerificationSubjects exposes exact historical locations for an operator to
// verify externally. Equal host pins do not establish equivalence of aliases.
func VerificationSubjects(record Record, current Identity) ([]VerificationSubject, error) {
	if err := record.validate(); err != nil {
		return nil, err
	}
	return verificationSubjects(record, current)
}

func verificationSubjects(record Record, current Identity) ([]VerificationSubject, error) {
	identities := []Identity{record.Identity, current}
	for _, proof := range record.Attestations {
		identities = append(identities, proof.Identity)
	}
	seen := map[string]bool{}
	result := []VerificationSubject{}
	for _, identity := range identities {
		if err := identity.validate(); err != nil {
			return nil, err
		}
		material, _ := hex.DecodeString(identity.KeyDigest)
		subject := VerificationSubject{
			Host: identity.Host, Port: identity.Port, Username: identity.Username,
			KeyFingerprint:   "SHA256:" + base64.RawStdEncoding.EncodeToString(material),
			HostFingerprints: slices.Clone(identity.HostFingerprints),
		}
		id, err := Digest(subject)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id], subject.ID = true, id
		result = append(result, subject)
	}
	slices.SortFunc(result, func(a, b VerificationSubject) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

func validateEvidence(prefix Record, decision Attestation) error {
	if !validDigest(decision.ContextDigest) {
		return errors.New("SSH key revocation requires a deletion-context digest")
	}
	subjects, err := verificationSubjects(prefix, decision.Identity)
	if err != nil {
		return err
	}
	if len(decision.Coverage) != len(subjects) {
		return errors.New("SSH key revocation requires evidence for every historical location")
	}
	for index, proof := range decision.Coverage {
		if proof.SubjectID != subjects[index].ID || !proof.Absent {
			return errors.New("SSH key revocation evidence does not match the complete location set")
		}
		switch proof.Method {
		case "provider_console", "independent_admin_session", "decommissioned_location":
		default:
			return errors.New("SSH key revocation evidence requires an external verification method")
		}
		if err := validateReason(proof.Reason); err != nil {
			return err
		}
	}
	return nil
}
