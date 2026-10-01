package keycleanup

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

const ResourceKind = "key_revocation"

const recordType = "key_revocation.v2"
const recordVersion = 2

type Status string

const (
	Intent    Status = "intent"
	Confirmed Status = "confirmed"
	Attested  Status = "attested"
)

var (
	ErrReconciliationRequired = errors.New("SSH key revocation requires operator reconciliation before authentication")
	ErrStaleGeneration        = errors.New("SSH key revocation generation changed; reload before reconciling")
)

type Record struct {
	Version      int           `json:"version"`
	Identity     Identity      `json:"identity"`
	Generation   string        `json:"generation"`
	Status       Status        `json:"status"`
	Attestations []Attestation `json:"attestations,omitempty"`
}

type Attestation struct {
	Identity      Identity          `json:"identity"`
	ContextDigest string            `json:"deletion_context_digest"`
	Coverage      []AbsenceEvidence `json:"coverage"`
	Reason        string            `json:"reason"`
}

type Entry struct {
	ResourceID int64  `json:"resource_id"`
	Record     Record `json:"record"`
}

func newGeneration() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (record Record) validate() error {
	if record.Version != recordVersion {
		return errors.New("unsupported SSH key revocation record version")
	}
	if err := record.Identity.validate(); err != nil {
		return err
	}
	generation, err := hex.DecodeString(record.Generation)
	if err != nil || len(generation) != 16 || hex.EncodeToString(generation) != record.Generation {
		return errors.New("invalid SSH key revocation generation")
	}
	switch record.Status {
	case Intent, Confirmed:
		if len(record.Attestations) != 0 {
			return errors.New("unexpected SSH key revocation attestation")
		}
	case Attested:
		if len(record.Attestations) == 0 {
			return errors.New("missing SSH key revocation attestation")
		}
		for index, attestation := range record.Attestations {
			if err := attestation.Identity.validate(); err != nil {
				return err
			}
			if err := validateReason(attestation.Reason); err != nil {
				return err
			}
			prefix := Record{Identity: record.Identity, Attestations: record.Attestations[:index]}
			if !prefix.overlaps(attestation.Identity) {
				return errors.New("disconnected SSH key revocation attestation identity")
			}
			if err := validateEvidence(prefix, attestation); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid SSH key revocation status")
	}
	return nil
}

func validateReason(reason string) error {
	if reason == "" || reason != strings.TrimSpace(reason) || len(reason) > 2000 {
		return errors.New("SSH key revocation attestation requires a bounded reason")
	}
	return nil
}

func (record Record) overlaps(identity Identity) bool {
	if record.Identity.overlaps(identity) {
		return true
	}
	for _, attestation := range record.Attestations {
		if attestation.Identity.overlaps(identity) {
			return true
		}
	}
	return false
}

func parseResource(resource resourcecontract.CredentialResource) (Entry, error) {
	var record Record
	decoder := json.NewDecoder(bytes.NewBufferString(resource.PublicData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Entry{}, errors.New("invalid SSH key revocation record data")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Entry{}, errors.New("invalid trailing SSH key revocation record data")
	}
	if err := record.validate(); err != nil {
		return Entry{}, err
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(canonical, []byte(resource.PublicData)) {
		return Entry{}, errors.New("noncanonical SSH key revocation record data")
	}
	digest, err := Digest(record.Identity)
	if err != nil {
		return Entry{}, err
	}
	if resource.ID < 1 || resource.ResourceType != recordType || resource.Name != resourceName(digest) || resource.Fingerprint != digest {
		return Entry{}, errors.New("SSH key revocation record identity mismatch")
	}
	return Entry{ResourceID: resource.ID, Record: record}, nil
}

func resourceName(digest string) string { return "key-revocation:" + digest }

func reconciliationError(entry Entry) error {
	return fmt.Errorf("%w (record %d, generation %s)", ErrReconciliationRequired, entry.ResourceID, entry.Record.Generation)
}
