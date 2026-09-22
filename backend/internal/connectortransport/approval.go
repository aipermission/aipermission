package connectortransport

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

var ErrApprovalChanged = errors.New("connector transport approval dependency changed before use")

type Approved map[string]actions.ResolvedDependency

func NewApproved(dependencies []actions.ResolvedDependency) Approved {
	approved := make(Approved, len(dependencies))
	for _, dependency := range dependencies {
		approved[key(dependency.Purpose, dependency.Target.Ref)] = dependency
	}
	return approved
}

func (approved Approved) Acquire(ctx context.Context, runtime Runtime, purpose, targetRef string) (func(), error) {
	if runtime.Database == nil || runtime.AcquireDelivery == nil {
		return nil, errors.New("database runtime is not available")
	}
	release := func() {}
	if !connectors.DeliveryAdmissionHeld(ctx, runtime.Admission) {
		var err error
		release, err = runtime.AcquireDelivery(ctx)
		if err != nil {
			if release != nil {
				release()
			}
			return nil, err
		}
		if release == nil {
			return nil, errors.New("connector delivery admission did not return a release function")
		}
	}
	if approved == nil {
		return release, nil
	}
	expected, ok := approved[key(purpose, targetRef)]
	if !ok {
		release()
		return nil, ErrApprovalChanged
	}
	currentTarget, currentProfile, err := connectortargets.NewStore(runtime.Database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil || !reflect.DeepEqual(currentTarget, expected.Target) || !reflect.DeepEqual(currentProfile, expected.Profile) {
		release()
		return nil, ErrApprovalChanged
	}
	return release, nil
}

func key(purpose, targetRef string) string {
	return strings.TrimSpace(purpose) + "\x00" + strings.TrimSpace(targetRef)
}
