package api

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type vaultEnvironmentSnapshot struct {
	RuntimeID              int64
	TargetID               int64
	ProfileID              int64
	ConnectorKind          string
	TargetContextHash      string
	PeerIdentities         []string
	Items                  []projectvault.SessionItem
	EnvironmentContentHash string
}

func buildVaultEnvironmentSnapshot(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	runtimeID int64,
	selections []projectvault.SessionSelection,
) (vaultEnvironmentSnapshot, error) {
	targets := connectortargets.NewStore(runtime.database)
	target, profile, surface, err := targets.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return vaultEnvironmentSnapshot{}, err
	}
	if surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return vaultEnvironmentSnapshot{}, errors.New("Vault environments require a live console runtime")
	}
	if err := requireSessionEnvironmentCapability(ctx, server, runtime, runtimeID); err != nil {
		return vaultEnvironmentSnapshot{}, err
	}
	store, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
	if err != nil {
		return vaultEnvironmentSnapshot{}, err
	}
	resolved, err := store.SnapshotSession(ctx, selections)
	if err != nil {
		return vaultEnvironmentSnapshot{}, err
	}
	defer resolved.Destroy()
	targetHash, err := currentVaultTargetContextHash(ctx, runtime, target.ID, profile.ID)
	if err != nil {
		return vaultEnvironmentSnapshot{}, err
	}
	peerIdentities, err := expectedLiveConsolePeerIdentities(ctx, server, runtime, surface)
	if err != nil {
		return vaultEnvironmentSnapshot{}, err
	}
	return vaultEnvironmentSnapshot{
		RuntimeID:              runtimeID,
		TargetID:               target.ID,
		ProfileID:              profile.ID,
		ConnectorKind:          target.ConnectorKind,
		TargetContextHash:      targetHash,
		PeerIdentities:         peerIdentities,
		Items:                  append([]projectvault.SessionItem(nil), resolved.Items...),
		EnvironmentContentHash: resolved.ContentHash,
	}, nil
}

func expectedLiveConsolePeerIdentities(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	surface connectortargets.RuntimeSurface,
) ([]string, error) {
	capability, err := sessionEnvironmentCapabilityFor(ctx, server, runtime, surface.ID)
	if err != nil {
		return nil, err
	}
	adapter, _ := server.connectorAPIAdapterFor(surface.ConnectorKind).(connectorapi.LiveConsolePeerIdentityAdapter)
	if adapter == nil {
		if capability.SessionEnvironmentPeerIdentityRequired() {
			return nil, errors.New("this connector requires a peer identity adapter for Vault session environments")
		}
		return nil, nil
	}
	items, err := adapter.ExpectedLiveConsolePeerIdentities(ctx, server, runtime, surface.ID)
	if err != nil {
		return nil, err
	}
	identities := normalizedIdentities(items)
	if capability.SessionEnvironmentPeerIdentityRequired() && len(identities) == 0 {
		return nil, errors.New("this connector has no trusted peer identities for Vault session environments")
	}
	return identities, nil
}

func newVaultEnvironmentPreparer(
	server *Server,
	runtime *databaseRuntime,
	snapshot vaultEnvironmentSnapshot,
	selections []projectvault.SessionSelection,
	authorize func(context.Context) error,
	finalize func(context.Context, console.SessionHandle) error,
) console.EnvironmentPreparer {
	return func(ctx context.Context, actualPeerIdentity string) (console.EnvironmentPreparation, error) {
		release, err := runtime.vaultDelivery.acquireDelivery(ctx)
		if err != nil {
			return console.EnvironmentPreparation{}, err
		}
		fail := func(err error) (console.EnvironmentPreparation, error) {
			release()
			return console.EnvironmentPreparation{}, err
		}
		if authorize != nil {
			if err := authorize(ctx); err != nil {
				return fail(err)
			}
		}
		if err := validateVaultEnvironmentContext(ctx, server, runtime, snapshot, actualPeerIdentity); err != nil {
			return fail(err)
		}
		store, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
		if err != nil {
			return fail(err)
		}
		resolved, err := store.ResolveSession(ctx, selections)
		if err != nil {
			return fail(err)
		}
		if resolved.ContentHash != snapshot.EnvironmentContentHash {
			resolved.Destroy()
			return fail(staleVaultContext("Vault items changed before secret delivery"))
		}
		return console.EnvironmentPreparation{
			Environment: resolved.Environment,
			Release:     release,
			PostValidate: func(validateCtx context.Context) error {
				if authorize != nil {
					if err := authorize(validateCtx); err != nil {
						return err
					}
				}
				if err := validateVaultEnvironmentContext(validateCtx, server, runtime, snapshot, actualPeerIdentity); err != nil {
					return err
				}
				if err := store.RevalidateSession(validateCtx, snapshot.Items); err != nil {
					return staleVaultContext("Vault items changed during secret delivery")
				}
				return nil
			},
			Finalize: finalize,
		}, nil
	}
}

func validateVaultEnvironmentContext(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	snapshot vaultEnvironmentSnapshot,
	actualPeerIdentity string,
) error {
	targetHash, err := currentVaultTargetContextHash(ctx, runtime, snapshot.TargetID, snapshot.ProfileID)
	if err != nil || targetHash != snapshot.TargetContextHash {
		return staleVaultContext("target or credential profile changed before secret delivery")
	}
	surface, err := connectortargets.NewStore(runtime.database).GetRuntimeSurface(ctx, snapshot.RuntimeID)
	if err != nil || surface.TargetID != snapshot.TargetID || surface.ProfileID != snapshot.ProfileID ||
		surface.ConnectorKind != snapshot.ConnectorKind ||
		surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return staleVaultContext("connector runtime changed before secret delivery")
	}
	currentPeers, err := expectedLiveConsolePeerIdentities(ctx, server, runtime, surface)
	if err != nil || !equalStrings(currentPeers, snapshot.PeerIdentities) {
		return staleVaultContext("connector peer trust changed before secret delivery")
	}
	if len(snapshot.PeerIdentities) > 0 && !containsString(snapshot.PeerIdentities, actualPeerIdentity) {
		return staleVaultContext("connected peer identity does not match the approved Vault context")
	}
	return nil
}

func normalizedIdentities(values []string) []string {
	seen := map[string]bool{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func vaultEnvironmentSnapshotFromApproval(approval vaultApprovalContext) vaultEnvironmentSnapshot {
	return vaultEnvironmentSnapshot{
		RuntimeID:              approval.RuntimeID,
		TargetID:               approval.TargetID,
		ProfileID:              approval.ProfileID,
		ConnectorKind:          approval.ConnectorKind,
		TargetContextHash:      approval.TargetContextHash,
		PeerIdentities:         append([]string(nil), approval.ExpectedPeerIdentities...),
		Items:                  append([]projectvault.SessionItem(nil), approval.Items...),
		EnvironmentContentHash: approval.EnvironmentContentHash,
	}
}

func validateSnapshotIdentity(snapshot vaultEnvironmentSnapshot) error {
	if snapshot.RuntimeID < 1 || snapshot.TargetID < 1 || snapshot.ProfileID < 1 ||
		snapshot.ConnectorKind == "" || snapshot.TargetContextHash == "" ||
		snapshot.EnvironmentContentHash == "" || len(snapshot.Items) == 0 {
		return fmt.Errorf("Vault environment snapshot is incomplete")
	}
	return nil
}
