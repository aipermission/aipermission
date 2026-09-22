package connectorports

import (
	"context"
	"errors"
	"sort"
)

var (
	ErrPeerTrustChangeRequired = errors.New("connector peer trust change is required")
	ErrWorkspaceLocked         = errors.New("database is locked")
	ErrPeerTrustUnavailable    = errors.New("connector peer trust coordinator is unavailable")
)

type PeerTrustWorkspace struct {
	Identifier       string
	AcquireExclusive func(context.Context) (func(), error)
	InvalidateAll    func(context.Context, string) error
}

type PeerTrustCoordinator struct {
	snapshot func() []PeerTrustWorkspace
}

func NewPeerTrustCoordinator(snapshot func() []PeerTrustWorkspace) *PeerTrustCoordinator {
	return &PeerTrustCoordinator{snapshot: snapshot}
}

func (coordinator *PeerTrustCoordinator) Change(ctx context.Context, change func() error) error {
	if change == nil {
		return ErrPeerTrustChangeRequired
	}
	if coordinator == nil || coordinator.snapshot == nil {
		return ErrPeerTrustUnavailable
	}
	workspaces := append([]PeerTrustWorkspace(nil), coordinator.snapshot()...)
	if len(workspaces) == 0 {
		return ErrWorkspaceLocked
	}
	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Identifier < workspaces[j].Identifier
	})
	releases := make([]func(), 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.AcquireExclusive == nil || workspace.InvalidateAll == nil {
			releasePeerTrustLocks(releases)
			return ErrPeerTrustUnavailable
		}
		release, err := workspace.AcquireExclusive(ctx)
		if err != nil {
			if release != nil {
				release()
			}
			releasePeerTrustLocks(releases)
			return err
		}
		if release == nil {
			releasePeerTrustLocks(releases)
			return ErrPeerTrustUnavailable
		}
		releases = append(releases, release)
	}
	defer releasePeerTrustLocks(releases)
	for _, workspace := range workspaces {
		if err := workspace.InvalidateAll(ctx, "connector peer trust changed; send a fresh Vault request"); err != nil {
			return err
		}
	}
	return change()
}

func releasePeerTrustLocks(releases []func()) {
	for index := len(releases) - 1; index >= 0; index-- {
		releases[index]()
	}
}
