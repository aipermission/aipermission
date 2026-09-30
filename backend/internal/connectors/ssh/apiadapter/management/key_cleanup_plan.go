package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"golang.org/x/crypto/ssh"
)

type keyCleanupGroup struct {
	Identity       keycleanup.Identity
	PublicKey      string
	ConnectionHost string
}

// Build every shared-key group from public snapshots before any intent or secret
// delivery. An invalid later profile must not follow a successful remote change.
func planKeyCleanup(ctx context.Context, gateway connectorapi.PeerIdentityGateway, runtime connectorapi.ConnectorDataRuntime, target connectorapi.Target, profiles []connectors.CredentialProfileView) ([]keyCleanupGroup, error) {
	if len(profiles) == 0 {
		return nil, errors.New("remote SSH key cleanup requires a saved credential profile")
	}
	store, err := keyStore(runtime)
	if err != nil {
		return nil, err
	}
	host := strings.TrimSpace(stringConfigValue(target.Config, "host"))
	port := intConfigValue(target.Config, "port", 22)
	fingerprints, err := execution.TrustedHostFingerprints(gateway.ConnectorTrustStorePath(), net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	configDigest, err := keycleanup.Digest(target.Config)
	if err != nil {
		return nil, err
	}
	profiles = slices.Clone(profiles)
	slices.SortFunc(profiles, func(a, b connectors.CredentialProfileView) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	groups := []keyCleanupGroup{}
	indexes := map[string]int{}
	for _, profile := range profiles {
		if profile.TargetID != target.ID || profile.ConnectorKind != target.ConnectorKind {
			return nil, errors.New("SSH key cleanup profile does not belong to this target")
		}
		key, err := store.Get(ctx, int64ConfigValue(profile.Public, "ssh_key_id"))
		if err != nil {
			return nil, err
		}
		parsed, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
		if err != nil || len(options) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
			return nil, errors.New("SSH key cleanup requires a single canonical public key")
		}
		digest := keyMaterialDigest(parsed)
		username := strings.TrimSpace(stringConfigValue(profile.Public, "username"))
		groupKey := username + "\x00" + digest
		index, exists := indexes[groupKey]
		if !exists {
			index = len(groups)
			indexes[groupKey] = index
			groups = append(groups, keyCleanupGroup{
				Identity: keycleanup.Identity{
					TargetID: target.ID, TargetRevision: target.UpdatedAt, ConfigDigest: configDigest,
					Host: host, Port: port, Username: username, KeyDigest: digest, HostFingerprints: fingerprints,
				},
				PublicKey:      string(ssh.MarshalAuthorizedKey(parsed)),
				ConnectionHost: host,
			})
		}
		publicDigest, err := keycleanup.Digest(profile.Public)
		if err != nil {
			return nil, err
		}
		groups[index].Identity.Profiles = append(groups[index].Identity.Profiles, keycleanup.ProfileIdentity{
			ID: profile.ID, Revision: profile.UpdatedAt, SecretRevision: profile.SecretRevision,
			PublicDigest: publicDigest, KeyID: key.ID, KeyRevision: key.UpdatedAt,
		})
	}
	for index := range groups {
		identity, err := keycleanup.NewIdentity(groups[index].Identity)
		if err != nil {
			return nil, err
		}
		groups[index].Identity = identity
	}
	return groups, nil
}

func keyMaterialDigest(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return hex.EncodeToString(sum[:])
}
