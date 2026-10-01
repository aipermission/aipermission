package rolejournal

import (
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const CapabilityName = "postgres_role_journal"

func (*Journal) ConnectorRuntimeCapability() string { return CapabilityName }

// FromRuntime grants only journal operations. It exposes no target lookup,
// credential decryption, console manager or arbitrary database handle.
func FromRuntime(runtime connectors.RuntimeContext) (*Journal, error) {
	journal, ok := runtime.Capability(CapabilityName).(*Journal)
	if !ok || journal == nil || journal.store == nil {
		return nil, errors.New("managed Postgres role journal is unavailable")
	}
	return journal, nil
}
