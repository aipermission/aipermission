package conformance_test

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

type postgresOperatorCapabilities struct {
	connectors.RuntimeCapabilityResolver
	journal *rolejournal.Journal
}

func (capabilities postgresOperatorCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == rolejournal.CapabilityName {
		return capabilities.journal
	}
	return capabilities.RuntimeCapabilityResolver.RuntimeCapability(name)
}
