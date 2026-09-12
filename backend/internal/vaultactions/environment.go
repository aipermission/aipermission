package vaultactions

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type environmentSnapshot struct {
	RuntimeID              int64
	TargetID               int64
	ProfileID              int64
	ConnectorKind          string
	TargetContextHash      string
	PeerIdentities         []string
	Items                  []projectvault.SessionItem
	EnvironmentContentHash string
}

type EnvironmentPlan struct {
	Items                  []projectvault.SessionItem
	EnvironmentContentHash string
	Prepare                EnvironmentPreparer
}

type EnvironmentSessionHandle struct {
	ID         int64
	RuntimeID  int64
	Generation int64
}

type EnvironmentPreparation struct {
	Environment  *sessionenv.Envelope
	Release      func()
	PostValidate func(context.Context) error
	Finalize     func(context.Context, EnvironmentSessionHandle) error
}

type EnvironmentPreparer func(context.Context, string) (EnvironmentPreparation, error)

func (r *Runtime) BuildEnvironmentPlan(
	ctx context.Context,
	runtimeID int64,
	selections []projectvault.SessionSelection,
) (EnvironmentPlan, error) {
	if err := r.validate(); err != nil {
		return EnvironmentPlan{}, err
	}
	snapshot, err := r.buildEnvironmentSnapshot(ctx, runtimeID, selections)
	if err != nil {
		return EnvironmentPlan{}, err
	}
	finalize := func(finalizeCtx context.Context, handle EnvironmentSessionHandle) error {
		if err := r.sessionItems.RecordSessionItems(finalizeCtx, handle.ID, snapshot.Items); err != nil {
			return err
		}
		return r.sessionItems.MarkSessionItemsUsed(finalizeCtx, snapshot.Items)
	}
	return EnvironmentPlan{
		Items:                  append([]projectvault.SessionItem(nil), snapshot.Items...),
		EnvironmentContentHash: snapshot.EnvironmentContentHash,
		Prepare:                r.environmentPreparer(snapshot, selections, nil, finalize),
	}, nil
}

func (r *Runtime) buildEnvironmentSnapshot(
	ctx context.Context,
	runtimeID int64,
	selections []projectvault.SessionSelection,
) (environmentSnapshot, error) {
	targets := connectortargets.NewStore(r.database)
	target, profile, surface, err := targets.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return environmentSnapshot{}, err
	}
	if surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return environmentSnapshot{}, errors.New("Vault environments require a live console runtime")
	}
	if _, err := r.connector.SessionEnvironmentVersion(ctx, runtimeID); err != nil {
		return environmentSnapshot{}, err
	}
	resolved, err := r.sessionItems.SnapshotSession(ctx, selections)
	if err != nil {
		return environmentSnapshot{}, err
	}
	defer resolved.Destroy()
	targetHash, err := r.targetContextHash(ctx, target.ID, profile.ID)
	if err != nil {
		return environmentSnapshot{}, err
	}
	peerExpectation, err := r.connector.ExpectedPeerIdentities(ctx, surface)
	if err != nil {
		return environmentSnapshot{}, err
	}
	peerIdentities := normalizeIdentities(peerExpectation.Items)
	if peerExpectation.Required && len(peerIdentities) == 0 {
		return environmentSnapshot{}, errors.New("this connector has no trusted peer identities for Vault session environments")
	}
	return environmentSnapshot{
		RuntimeID: runtimeID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: target.ConnectorKind, TargetContextHash: targetHash,
		PeerIdentities:         peerIdentities,
		Items:                  append([]projectvault.SessionItem(nil), resolved.Items...),
		EnvironmentContentHash: resolved.ContentHash,
	}, nil
}

func (r *Runtime) environmentPreparer(
	snapshot environmentSnapshot,
	selections []projectvault.SessionSelection,
	authorize func(context.Context) error,
	finalize func(context.Context, EnvironmentSessionHandle) error,
) EnvironmentPreparer {
	return func(ctx context.Context, actualPeerIdentity string) (EnvironmentPreparation, error) {
		release, err := r.delivery.AcquireDelivery(ctx)
		if err != nil {
			return EnvironmentPreparation{}, err
		}
		fail := func(err error) (EnvironmentPreparation, error) {
			release()
			return EnvironmentPreparation{}, err
		}
		if authorize != nil {
			if err := authorize(ctx); err != nil {
				return fail(err)
			}
		}
		if err := r.validateEnvironmentContext(ctx, snapshot, actualPeerIdentity); err != nil {
			return fail(err)
		}
		resolved, err := r.sessionItems.ResolveSession(ctx, selections)
		if err != nil {
			return fail(err)
		}
		if resolved.ContentHash != snapshot.EnvironmentContentHash {
			resolved.Destroy()
			return fail(staleContext("Vault items changed before secret delivery"))
		}
		return EnvironmentPreparation{
			Environment: resolved.Environment, Release: release,
			PostValidate: func(validateCtx context.Context) error {
				if authorize != nil {
					if err := authorize(validateCtx); err != nil {
						return err
					}
				}
				if err := r.validateEnvironmentContext(validateCtx, snapshot, actualPeerIdentity); err != nil {
					return err
				}
				if err := r.sessionItems.RevalidateSession(validateCtx, snapshot.Items); err != nil {
					return staleContext("Vault items changed during secret delivery")
				}
				return nil
			},
			Finalize: finalize,
		}, nil
	}
}

func (r *Runtime) validateEnvironmentContext(
	ctx context.Context,
	snapshot environmentSnapshot,
	actualPeerIdentity string,
) error {
	targetHash, err := r.targetContextHash(ctx, snapshot.TargetID, snapshot.ProfileID)
	if err != nil || targetHash != snapshot.TargetContextHash {
		return staleContext("target or credential profile changed before secret delivery")
	}
	surface, err := connectortargets.NewStore(r.database).GetRuntimeSurface(ctx, snapshot.RuntimeID)
	if err != nil || surface.TargetID != snapshot.TargetID || surface.ProfileID != snapshot.ProfileID ||
		surface.ConnectorKind != snapshot.ConnectorKind ||
		surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return staleContext("connector runtime changed before secret delivery")
	}
	currentExpectation, err := r.connector.ExpectedPeerIdentities(ctx, surface)
	currentPeers := normalizeIdentities(currentExpectation.Items)
	if err != nil || (currentExpectation.Required && len(currentPeers) == 0) ||
		!equalStrings(currentPeers, snapshot.PeerIdentities) {
		return staleContext("connector peer trust changed before secret delivery")
	}
	if len(snapshot.PeerIdentities) > 0 && !containsString(snapshot.PeerIdentities, actualPeerIdentity) {
		return staleContext("connected peer identity does not match the approved Vault context")
	}
	return nil
}

func snapshotFromApproval(approval vaultrequests.ApprovalContext) environmentSnapshot {
	return environmentSnapshot{
		RuntimeID: approval.RuntimeID, TargetID: approval.TargetID, ProfileID: approval.ProfileID,
		ConnectorKind: approval.ConnectorKind, TargetContextHash: approval.TargetContextHash,
		PeerIdentities:         append([]string(nil), approval.ExpectedPeerIdentities...),
		Items:                  append([]projectvault.SessionItem(nil), approval.Items...),
		EnvironmentContentHash: approval.EnvironmentContentHash,
	}
}

func validateSnapshot(snapshot environmentSnapshot) error {
	if snapshot.RuntimeID < 1 || snapshot.TargetID < 1 || snapshot.ProfileID < 1 ||
		snapshot.ConnectorKind == "" || snapshot.TargetContextHash == "" ||
		snapshot.EnvironmentContentHash == "" || len(snapshot.Items) == 0 {
		return fmt.Errorf("Vault environment snapshot is incomplete")
	}
	return nil
}

func normalizeIdentities(values []string) []string {
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
