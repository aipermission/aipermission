package management

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"golang.org/x/crypto/ssh"
)

var errKeyCleanupPreflight = errors.New("SSH key cleanup prerequisites are incomplete or invalid")

func cleanupTargetKeys(ctx context.Context, gateway connectorapi.PeerIdentityGateway, runtime connectorapi.ConnectorDataRuntime, target connectorapi.Target, profiles []connectors.CredentialProfileView) (int64, error) {
	groups, err := planKeyCleanup(ctx, gateway, runtime, target, profiles)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", errKeyCleanupPreflight, err)
	}
	journal := keycleanup.New(runtime.CredentialResources(keycleanup.ResourceKind))
	entries := make([]keycleanup.Entry, len(groups))
	dispatch := make([]bool, len(groups))
	for index, group := range groups {
		entries[index], dispatch[index], err = journal.Begin(ctx, group.Identity)
		if err != nil {
			return 0, err
		}
	}
	store, err := keyStore(runtime)
	if err != nil {
		return 0, err
	}
	var removedGroups int64
	for index, group := range groups {
		if !dispatch[index] {
			continue
		}
		key, err := store.GetPrivateKey(ctx, group.Identity.Profiles[0].KeyID)
		if err != nil {
			return removedGroups, err
		}
		signer, err := ssh.ParsePrivateKey([]byte(key.PrivateKey))
		if err != nil || keyMaterialDigest(signer.PublicKey()) != group.Identity.KeyDigest {
			return removedGroups, errors.New("SSH key material changed after cleanup planning")
		}
		commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, err := execution.RunCommand(commandCtx, execution.Target{
			Host: group.ConnectionHost, Port: group.Identity.Port, Username: group.Identity.Username,
			PrivateKey: key.PrivateKey, KnownHostsPath: gateway.ConnectorTrustStorePath(),
		}, removeAuthorizedKeyCommand(group.PublicKey))
		cancel()
		if err != nil {
			return removedGroups, errors.New("remote SSH key cleanup outcome is unconfirmed")
		}
		count, err := confirmedKeyRemovalCount(result)
		if err != nil {
			return removedGroups, err
		}
		if _, err := journal.Confirm(ctx, entries[index]); err != nil {
			return removedGroups, err
		}
		if count > 0 {
			removedGroups++
		}
	}
	return removedGroups, nil
}

func confirmedKeyRemovalCount(result execution.Result) (uint64, error) {
	const prefix = "aipermission_key_removed="
	line := strings.TrimSuffix(result.Stdout, "\n")
	if !result.DispatchStarted || result.ExitCode != 0 || result.Stderr != "" ||
		!strings.HasSuffix(result.Stdout, "\n") || !strings.HasPrefix(line, prefix) {
		return 0, errors.New("remote SSH key cleanup outcome is unconfirmed")
	}
	value := strings.TrimPrefix(line, prefix)
	count, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(count, 10) != value {
		return 0, errors.New("remote SSH key cleanup outcome is unconfirmed")
	}
	return count, nil
}
