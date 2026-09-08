package connectors

import "strings"

// NetworkTransportDependencies binds an action to the connector-owned
// transport target when its endpoint is reached through another connector.
// Core resolves and fingerprints only dependencies declared by connectors.
func NetworkTransportDependencies(target TargetView) []ApprovalDependency {
	return transportDependencies(target, NetworkTransportCapabilityName, "direct")
}

// CommandTransportDependencies binds an action to the connector-owned command
// transport target selected by the connector configuration.
func CommandTransportDependencies(target TargetView) []ApprovalDependency {
	return transportDependencies(target, CommandTransportCapabilityName, "connector")
}

// UsesConnectorTransport reports whether a connection mode delegates work to
// the connector referenced by transport_target_ref. Direct transport is the
// only gateway-owned mode; connector implementations may define any other
// reviewed mode without teaching core its name.
func UsesConnectorTransport(mode string, defaultMode string) bool {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = defaultMode
	}
	return mode != "direct"
}

func transportDependencies(target TargetView, purpose string, defaultMode string) []ApprovalDependency {
	if !UsesConnectorTransport(stringConfigValue(target.Config, "connection_mode"), defaultMode) {
		return nil
	}
	return []ApprovalDependency{{
		TargetRef: strings.TrimSpace(stringConfigValue(target.Config, "transport_target_ref")),
		Purpose:   purpose,
	}}
}

func stringConfigValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
