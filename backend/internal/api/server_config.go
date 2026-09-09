package api

// RuntimeConfiguration is the narrow process-configuration contract consumed
// by the gateway composition root. The API snapshots these values at startup;
// it does not own environment loading or configuration validation.
type RuntimeConfiguration interface {
	RuntimeDataPath() string
	RuntimeGatewaySecret() string
	RuntimeFrontendPort() string
	AllowsOrigin(string) bool
	PublicStatusMinimalWithGatewaySecret(string) map[string]any
	IsLocalhostHeader(string) bool
	IsLocalRemoteAddr(string) bool
}

type serverConfig struct {
	DataPath          string
	GatewaySecret     string
	FrontendPort      string
	publicStatus      func(string) map[string]any
	allowsOrigin      func(string) bool
	isLocalhostHeader func(string) bool
	isLocalRemoteAddr func(string) bool
}

func snapshotRuntimeConfiguration(source RuntimeConfiguration) serverConfig {
	if source == nil {
		return serverConfig{}
	}
	return serverConfig{
		DataPath:          source.RuntimeDataPath(),
		GatewaySecret:     source.RuntimeGatewaySecret(),
		FrontendPort:      source.RuntimeFrontendPort(),
		publicStatus:      source.PublicStatusMinimalWithGatewaySecret,
		allowsOrigin:      source.AllowsOrigin,
		isLocalhostHeader: source.IsLocalhostHeader,
		isLocalRemoteAddr: source.IsLocalRemoteAddr,
	}
}

func (c serverConfig) AllowsOrigin(origin string) bool {
	return c.allowsOrigin != nil && c.allowsOrigin(origin)
}

func (c serverConfig) IsLocalhostHeader(value string) bool {
	return c.isLocalhostHeader != nil && c.isLocalhostHeader(value)
}

func (c serverConfig) IsLocalRemoteAddr(value string) bool {
	return c.isLocalRemoteAddr != nil && c.isLocalRemoteAddr(value)
}

func (c serverConfig) PublicStatusMinimal() map[string]any {
	if c.publicStatus == nil {
		return map[string]any{}
	}
	status := c.publicStatus(c.GatewaySecret)
	result := make(map[string]any, len(status))
	for key, value := range status {
		result[key] = value
	}
	return result
}
