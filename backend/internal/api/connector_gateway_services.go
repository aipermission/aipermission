package api

import (
	"errors"
	"path/filepath"
)

var errInvalidConnectorRuntime = errors.New("invalid connector runtime")

func (s *Server) connectorTrustStorePath() string {
	return filepath.Join(filepath.Dir(s.config.DataPath), "connector_trust_store")
}

// ConnectorTrustStorePath exposes the gateway-owned local trust store path to
// connector adapters that pin external endpoint identity.
func (s *Server) ConnectorTrustStorePath() string {
	return s.connectorTrustStorePath()
}
