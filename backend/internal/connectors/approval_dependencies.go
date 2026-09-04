package connectors

import "strings"

// NetworkTransportDependencies binds an action to the connector-owned
// transport target when its endpoint is reached through another connector.
// Core resolves and fingerprints only dependencies declared by connectors.
func NetworkTransportDependencies(target TargetView) []ApprovalDependency {
	return transportDependencies(target, NetworkTransportCapabilityName)
}

// CommandTransportDependencies binds an action to the connector-owned command
// transport target selected by the connector configuration.
func CommandTransportDependencies(target TargetView) []ApprovalDependency {
	return transportDependencies(target, CommandTransportCapabilityName)
}

func transportDependencies(target TargetView, purpose string) []ApprovalDependency {
	if strings.TrimSpace(stringConfigValue(target.Config, "connection_mode")) != "over_ssh" {
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
