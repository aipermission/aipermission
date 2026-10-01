package rolejournal

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

const CleanupEvidenceCapabilityName = "postgres_role_cleanup_evidence"

// CleanupEvidence has no mutation methods. It cannot manufacture confirmations
// through a Journal or recover a mutable/secret resource store from core.
type CleanupEvidence struct {
	reader resourcecontract.CredentialResourceReader
}

func NewCleanupEvidence(reader resourcecontract.CredentialResourceReader) *CleanupEvidence {
	if resourcecontract.IsNilDependency(reader) {
		reader = nil
	}
	return &CleanupEvidence{reader: reader}
}

func (*CleanupEvidence) ConnectorRuntimeCapability() string { return CleanupEvidenceCapabilityName }

func CleanupEvidenceFrom(capabilities connectors.RuntimeCapabilityResolver) (*CleanupEvidence, error) {
	if resourcecontract.IsNilDependency(capabilities) {
		return nil, errors.New("managed Postgres cleanup evidence is unavailable")
	}
	evidence, ok := capabilities.RuntimeCapability(CleanupEvidenceCapabilityName).(*CleanupEvidence)
	if !ok || evidence == nil || evidence.reader == nil {
		return nil, errors.New("managed Postgres cleanup evidence is unavailable")
	}
	return evidence, nil
}

func (evidence *CleanupEvidence) get(ctx context.Context, id int64) (Entry, error) {
	if evidence == nil {
		return Entry{}, errors.New("managed Postgres cleanup evidence is unavailable")
	}
	return readEntry(ctx, evidence.reader, id)
}

// ConfirmedCleanup reads the immutable identity and rechecks the entire terminal
// snapshot. Neither absence nor rollback can confirm remote cleanup.
func (evidence *CleanupEvidence) ConfirmedCleanup(ctx context.Context, reference Reference) (Entry, bool, error) {
	entry, err := resolveReference(ctx, reference, evidence.get)
	if err != nil || entry.Record.Status != Cleaned {
		return Entry{}, false, err
	}
	confirmed, err := readCurrent(ctx, entry, evidence.get)
	return confirmed, err == nil, err
}
