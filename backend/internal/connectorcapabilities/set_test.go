package connectorcapabilities

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type capability string

func (value capability) ConnectorRuntimeCapability() string { return string(value) }

type nilCapability struct{}

func (*nilCapability) ConnectorRuntimeCapability() string { panic("typed nil capability was invoked") }

func TestMergePreservesProtectedSetAndCopiesBothMaps(t *testing.T) {
	base := Set{"protected": capability("protected")}
	provided := map[string]connectors.RuntimeCapability{"journal": capability("journal")}
	merged, err := Merge(base, provided)
	if err != nil || merged.RuntimeCapability("protected") != base["protected"] || merged.RuntimeCapability("journal") != provided["journal"] {
		t.Fatalf("merge=%#v err=%v", merged, err)
	}
	delete(base, "protected")
	delete(provided, "journal")
	if len(merged) != 2 || merged.RuntimeCapability("missing") != nil {
		t.Fatal("merged resolver aliases input maps or invents a capability")
	}
	if empty, err := Merge(nil, nil); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty composition=%#v err=%v", empty, err)
	}
}

func TestMergeRejectsInvalidProvidersWithoutChangingProtectedSet(t *testing.T) {
	base := Set{"protected": capability("protected")}
	for name, value := range map[string]connectors.RuntimeCapability{
		"bad-name": capability("bad-name"), "nil": nil, "typed_nil": (*nilCapability)(nil),
		"different": capability("declared"), "protected": capability("protected"),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := Merge(base, map[string]connectors.RuntimeCapability{name: value}); err == nil || got != nil || len(base) != 1 {
				t.Fatalf("invalid capability changed protected set: %#v %v", got, err)
			}
		})
	}
}
