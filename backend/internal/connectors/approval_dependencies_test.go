package connectors

import "testing"

func TestTransportDependenciesUseRuntimeConnectionModeDefaults(t *testing.T) {
	target := TargetView{Config: map[string]any{"transport_target_ref": "ssh:1:1"}}
	if dependencies := NetworkTransportDependencies(target); len(dependencies) != 0 {
		t.Fatalf("network transport default should be direct: %#v", dependencies)
	}
	dependencies := CommandTransportDependencies(target)
	if len(dependencies) != 1 || dependencies[0].TargetRef != "ssh:1:1" || dependencies[0].Purpose != CommandTransportCapabilityName {
		t.Fatalf("command transport default should be over SSH: %#v", dependencies)
	}
}
