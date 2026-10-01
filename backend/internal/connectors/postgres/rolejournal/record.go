package rolejournal

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

const ResourceKind = "managed_role_lifecycle"
const recordType = "managed_role_lifecycle.v1"
const maxRecordBytes = 4096

type Status string

const (
	ProvisionIntent Status = "provision_intent"
	Provisioned     Status = "provisioned"
	CleanupIntent   Status = "cleanup_intent"
	Cleaned         Status = "cleaned"
	RolledBack      Status = "rolled_back"
)

var (
	ErrReconciliationRequired = errors.New("managed Postgres role requires operator reconciliation")
	ErrStaleGeneration        = errors.New("managed Postgres role generation changed; reload before reconciling")
)

type Record struct {
	Version    int    `json:"version"`
	Intent     Intent `json:"intent"`
	RoleOID    uint32 `json:"role_oid"`
	Generation string `json:"generation"`
	Status     Status `json:"status"`
}

type Entry struct {
	ResourceID int64  `json:"resource_id,string"`
	Record     Record `json:"record"`
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (record Record) Validate() error {
	if record.Version != 1 || !validHex(record.Generation, 16) {
		return errors.New("invalid managed Postgres role record version or generation")
	}
	if err := record.Intent.validate(); err != nil {
		return err
	}
	if record.RoleOID != 0 && record.RoleOID == record.Intent.Anchor.SuccessorOID {
		return errors.New("managed Postgres role cannot be its ownership successor")
	}
	switch record.Status {
	case ProvisionIntent, RolledBack:
	case Provisioned, CleanupIntent, Cleaned:
		if record.RoleOID == 0 {
			return errors.New("managed Postgres role identity is not bound")
		}
	default:
		return errors.New("invalid managed Postgres role status")
	}
	return nil
}

func parseResource(resource resourcecontract.CredentialResource) (Entry, error) {
	if len(resource.PublicData) > maxRecordBytes {
		return Entry{}, errors.New("managed Postgres role record exceeds the size limit")
	}
	var record Record
	decoder := json.NewDecoder(bytes.NewBufferString(resource.PublicData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Entry{}, errors.New("invalid managed Postgres role record data")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Entry{}, errors.New("invalid trailing managed Postgres role record data")
	}
	if err := record.Validate(); err != nil {
		return Entry{}, err
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(canonical, []byte(resource.PublicData)) {
		return Entry{}, errors.New("noncanonical managed Postgres role record data")
	}
	if resource.ID < 1 || resource.ResourceType != recordType ||
		resource.Name != resourceName(record.Intent) || resource.Fingerprint != intentDigest(record.Intent) {
		return Entry{}, errors.New("managed Postgres role resource identity mismatch")
	}
	return Entry{ResourceID: resource.ID, Record: record}, nil
}

func resourceName(intent Intent) string { return "managed-role:" + intent.OperationID }
