package api

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
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

func (s *Server) connectorChangeVaultPeerTrust(ctx context.Context, change func() error) error {
	if change == nil {
		return errors.New("connector peer trust change is required")
	}
	runtimes := s.unlockedRuntimeSnapshot()
	if len(runtimes) == 0 {
		return errors.New("database is locked")
	}
	sort.Slice(runtimes, func(i, j int) bool {
		return runtimes[i].DatabaseIdentifier() < runtimes[j].DatabaseIdentifier()
	})
	releases := make([]func(), 0, len(runtimes))
	for _, runtime := range runtimes {
		release, err := runtime.SecurityPort().VaultDeliveryCoordinator().AcquireExclusive(ctx)
		if err != nil {
			for index := len(releases) - 1; index >= 0; index-- {
				releases[index]()
			}
			return err
		}
		releases = append(releases, release)
	}
	defer func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}()
	for _, runtime := range runtimes {
		if err := s.invalidateAllVaultSessions(
			ctx,
			runtime,
			"connector peer trust changed; send a fresh Vault request",
		); err != nil {
			return err
		}
	}
	return change()
}
