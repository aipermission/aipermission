package gatewayvault

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type SessionAuthorizationGuard func(context.Context, func() error, func() error) error
type SessionAuthorizerInstaller func(SessionAuthorizationGuard)
type SessionClosedHookInstaller func(func(context.Context, VaultSessionReference) error)

type SessionLifecycleRuntime struct {
	Database             *sql.DB
	Leases               vaultsessions.LeaseRevoker
	Sessions             vaultsessions.SessionCloser
	Principal            vaultsessions.PrincipalProvider
	Requests             func(context.Context) (RequestInvalidator, error)
	AcquireDelivery      func(context.Context) (func(), error)
	InstallAuthorizer    SessionAuthorizerInstaller
	InstallSessionClosed SessionClosedHookInstaller
}

type SessionLifecycle struct {
	runtime     SessionLifecycleRuntime
	invalidator *vaultsessions.Invalidator
}

func (component *Component) SessionLifecycle(runtime SessionLifecycleRuntime) (*SessionLifecycle, error) {
	if component == nil || runtime.Database == nil || runtime.Leases == nil || runtime.Sessions == nil ||
		runtime.Principal == nil || runtime.Requests == nil || runtime.AcquireDelivery == nil ||
		runtime.InstallAuthorizer == nil || runtime.InstallSessionClosed == nil {
		return nil, vaultsessions.ErrInvalidatorUnavailable
	}
	invalidator, err := vaultsessions.NewInvalidator(vaultsessions.InvalidatorDependencies{
		Persistence: vaultsessions.NewPersistence(runtime.Database),
		Leases:      runtime.Leases, Sessions: runtime.Sessions, Principal: runtime.Principal,
		Requests: func(ctx context.Context) (vaultsessions.RequestInvalidator, error) {
			return runtime.Requests(ctx)
		},
	})
	if err != nil {
		return nil, err
	}
	return &SessionLifecycle{runtime: runtime, invalidator: invalidator}, nil
}

func (lifecycle *SessionLifecycle) Configure() error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	lifecycle.runtime.InstallAuthorizer(func(ctx context.Context, authorize func() error, run func() error) error {
		release, err := lifecycle.runtime.AcquireDelivery(ctx)
		if err != nil {
			return err
		}
		if release == nil {
			return vaultsessions.ErrInvalidatorUnavailable
		}
		defer release()
		if err := authorize(); err != nil {
			return err
		}
		return run()
	})
	lifecycle.runtime.InstallSessionClosed(func(ctx context.Context, reference VaultSessionReference) error {
		return lifecycle.invalidator.SessionClosedReference(ctx, vaultsessions.Reference{
			SessionID: reference.SessionID, RuntimeID: reference.RuntimeID, Generation: reference.Generation,
		})
	})
	return nil
}

func (lifecycle *SessionLifecycle) InvalidateMutation(
	ctx context.Context,
	sessions []SessionReference,
	scope SessionMutationScope,
) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	references := make([]vaultsessions.Reference, len(sessions))
	for index, session := range sessions {
		references[index] = vaultsessions.Reference{
			SessionID: session.SessionID, RuntimeID: session.RuntimeID, Generation: session.Generation,
		}
	}
	return lifecycle.invalidator.InvalidateMutation(
		ctx, references, scope.ItemID, scope.BindingID,
		"Vault item or binding changed; send a fresh request",
	)
}

func (lifecycle *SessionLifecycle) FinishTokenInvalidation(ctx context.Context, tokenID int64, sessionIDs []int64) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	return lifecycle.invalidator.FinishTokenInvalidation(ctx, tokenID, sessionIDs)
}

func (lifecycle *SessionLifecycle) InvalidateProject(ctx context.Context, projectID int64, reason string) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	return lifecycle.invalidator.InvalidateProject(ctx, projectID, reason)
}

func (lifecycle *SessionLifecycle) InvalidateProjectReferences(ctx context.Context, projectID int64, reason string, references []SessionReference) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	items := make([]vaultsessions.Reference, len(references))
	for index, reference := range references {
		items[index] = vaultsessions.Reference{
			SessionID: reference.SessionID, RuntimeID: reference.RuntimeID, Generation: reference.Generation,
		}
	}
	return lifecycle.invalidator.InvalidateProjectReferences(ctx, items, projectID, reason)
}

func (lifecycle *SessionLifecycle) RecoverPendingFinalizations(ctx context.Context) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	return lifecycle.invalidator.RecoverPendingFinalizations(ctx)
}

func (lifecycle *SessionLifecycle) InvalidateRuntimes(ctx context.Context, runtimeIDs []int64, reason string) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	return lifecycle.invalidator.InvalidateRuntimes(ctx, runtimeIDs, reason)
}

func (lifecycle *SessionLifecycle) InvalidateTargetProfile(ctx context.Context, targetID, profileID int64, reason string) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	return lifecycle.invalidator.InvalidateTargetProfile(ctx, targetID, profileID, reason)
}

func (lifecycle *SessionLifecycle) InvalidateAll(ctx context.Context, reason string) error {
	if err := lifecycle.validate(); err != nil {
		return err
	}
	return lifecycle.invalidator.InvalidateAll(ctx, reason)
}

func (lifecycle *SessionLifecycle) validate() error {
	if lifecycle == nil || lifecycle.invalidator == nil || lifecycle.runtime.Leases == nil ||
		lifecycle.runtime.Sessions == nil || lifecycle.runtime.AcquireDelivery == nil ||
		lifecycle.runtime.InstallAuthorizer == nil || lifecycle.runtime.InstallSessionClosed == nil {
		return vaultsessions.ErrInvalidatorUnavailable
	}
	return nil
}
