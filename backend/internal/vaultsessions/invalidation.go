package vaultsessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

const defaultInvalidationTimeout = 15 * time.Second

var ErrInvalidatorUnavailable = errors.New("Vault session invalidator is unavailable")

type LeaseRevoker interface {
	RevokeSession(console.SessionHandle)
	RevokeToken(int64)
	Clear()
}

type SessionCloser interface {
	Close(context.Context, executionprincipal.Principal, int64) error
}

type RequestInvalidator interface {
	StalePendingForContext(context.Context, int64, int64, string) error
	StalePendingForProject(context.Context, int64, string) error
	StalePendingForRuntimes(context.Context, []int64, string) error
}

type RequestInvalidatorProvider func(context.Context) (RequestInvalidator, error)
type PrincipalProvider func() (executionprincipal.Principal, error)

type InvalidatorDependencies struct {
	Persistence    *Persistence
	Leases         LeaseRevoker
	Sessions       SessionCloser
	Principal      PrincipalProvider
	Requests       RequestInvalidatorProvider
	CleanupTimeout time.Duration
}

type Invalidator struct {
	persistence    *Persistence
	leases         LeaseRevoker
	sessions       SessionCloser
	principal      PrincipalProvider
	requests       RequestInvalidatorProvider
	cleanupTimeout time.Duration
}

func NewInvalidator(dependencies InvalidatorDependencies) (*Invalidator, error) {
	if dependencies.Persistence == nil || dependencies.Leases == nil || dependencies.Sessions == nil ||
		dependencies.Principal == nil || dependencies.Requests == nil {
		return nil, ErrInvalidatorUnavailable
	}
	timeout := dependencies.CleanupTimeout
	if timeout <= 0 {
		timeout = defaultInvalidationTimeout
	}
	return &Invalidator{
		persistence:    dependencies.Persistence,
		leases:         dependencies.Leases,
		sessions:       dependencies.Sessions,
		principal:      dependencies.Principal,
		requests:       dependencies.Requests,
		cleanupTimeout: timeout,
	}, nil
}

func (i *Invalidator) InvalidateMutation(
	ctx context.Context,
	references []Reference,
	itemID int64,
	bindingID int64,
	reason string,
) error {
	closeErr := i.closeReferences(ctx, references)
	requests, requestErr := i.requestInvalidator(ctx)
	if requestErr != nil {
		return errors.Join(closeErr, requestErr)
	}
	return errors.Join(closeErr, requests.StalePendingForContext(ctx, itemID, bindingID, reason))
}

func (i *Invalidator) InvalidateProject(ctx context.Context, projectID int64, reason string) error {
	if err := i.validate(); err != nil {
		return err
	}
	references, err := i.persistence.ActiveForProject(ctx, projectID)
	if err != nil {
		return err
	}
	return i.InvalidateProjectReferences(ctx, references, projectID, reason)
}

func (i *Invalidator) InvalidateProjectReferences(ctx context.Context, references []Reference, projectID int64, reason string) error {
	if err := i.validate(); err != nil {
		return err
	}
	closeErr := i.closeReferences(ctx, references)
	requests, requestErr := i.requestInvalidator(ctx)
	if requestErr != nil {
		return errors.Join(closeErr, requestErr)
	}
	return errors.Join(closeErr, requests.StalePendingForProject(ctx, projectID, reason))
}

func (i *Invalidator) InvalidateRuntimes(ctx context.Context, runtimeIDs []int64, reason string) error {
	if err := i.validate(); err != nil {
		return err
	}
	if len(runtimeIDs) == 0 {
		return nil
	}
	references, referenceErr := i.persistence.ActiveEnvironmentSessionsForRuntimes(ctx, runtimeIDs)
	var closeErr error
	if referenceErr == nil {
		closeErr = i.closeReferences(ctx, references)
	}
	requests, requestErr := i.requestInvalidator(ctx)
	if requestErr != nil {
		return errors.Join(referenceErr, closeErr, requestErr)
	}
	return errors.Join(referenceErr, closeErr, requests.StalePendingForRuntimes(ctx, runtimeIDs, reason))
}

func (i *Invalidator) InvalidateTargetProfile(
	ctx context.Context,
	targetID int64,
	profileID int64,
	reason string,
) error {
	if err := i.validate(); err != nil {
		return err
	}
	runtimeIDs, err := i.persistence.RuntimeIDsForTargetProfile(ctx, targetID, profileID)
	if err != nil {
		return err
	}
	return i.InvalidateRuntimes(ctx, runtimeIDs, reason)
}

func (i *Invalidator) InvalidateAll(ctx context.Context, reason string) error {
	if err := i.validate(); err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), i.cleanupTimeout)
	defer cancel()

	i.leases.Clear()
	runtimeIDs, runtimeErr := i.persistence.AllRuntimeIDs(cleanupCtx)
	var invalidateErr error
	if runtimeErr == nil {
		invalidateErr = i.InvalidateRuntimes(cleanupCtx, runtimeIDs, reason)
	}
	revokeErr := i.persistence.RevokeAll(cleanupCtx)
	return errors.Join(runtimeErr, invalidateErr, revokeErr)
}

func (i *Invalidator) FinishTokenInvalidation(ctx context.Context, tokenID int64, sessionIDs []int64) error {
	if err := i.validate(); err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), i.cleanupTimeout)
	defer cancel()
	i.leases.RevokeToken(tokenID)
	principal, err := i.principal()
	if err != nil {
		return err
	}
	var closeErrors []error
	for _, sessionID := range sessionIDs {
		if err := i.sessions.Close(cleanupCtx, principal, sessionID); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close token Vault session %d: %w", sessionID, err))
		}
	}
	return errors.Join(closeErrors...)
}

func (i *Invalidator) SessionClosed(ctx context.Context, handle console.SessionHandle) error {
	if err := i.validate(); err != nil {
		return err
	}
	i.leases.RevokeSession(handle)
	return i.persistence.Revoke(ctx, handle.ID, handle.Generation)
}

func (i *Invalidator) SessionClosedReference(ctx context.Context, reference Reference) error {
	return i.SessionClosed(ctx, console.SessionHandle{
		ID: reference.SessionID, RuntimeID: reference.RuntimeID, Generation: reference.Generation,
	})
}

func (i *Invalidator) closeReferences(ctx context.Context, references []Reference) error {
	if err := i.validate(); err != nil {
		return err
	}
	var closeErrors []error
	for _, reference := range references {
		handle := console.SessionHandle{
			ID: reference.SessionID, RuntimeID: reference.RuntimeID, Generation: reference.Generation,
		}
		i.leases.RevokeSession(handle)
		if err := i.persistence.Revoke(ctx, reference.SessionID, reference.Generation); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf(
				"revoke persisted Vault lease for session %d: %w", reference.SessionID, err,
			))
		}
	}
	if len(references) == 0 {
		return nil
	}
	principal, err := i.principal()
	if err != nil {
		return errors.Join(errors.Join(closeErrors...), err)
	}
	for _, reference := range references {
		if err := i.sessions.Close(ctx, principal, reference.SessionID); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close stale Vault session %d: %w", reference.SessionID, err))
		}
	}
	return errors.Join(closeErrors...)
}

func (i *Invalidator) requestInvalidator(ctx context.Context) (RequestInvalidator, error) {
	if err := i.validate(); err != nil {
		return nil, err
	}
	requests, err := i.requests(ctx)
	if err != nil {
		return nil, err
	}
	if requests == nil {
		return nil, ErrInvalidatorUnavailable
	}
	return requests, nil
}

func (i *Invalidator) validate() error {
	if i == nil || i.persistence == nil || i.leases == nil || i.sessions == nil ||
		i.principal == nil || i.requests == nil {
		return ErrInvalidatorUnavailable
	}
	return nil
}
