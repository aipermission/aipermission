package connectortransport

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
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

func (approved Approved) Acquire(ctx context.Context, runtime *workspaceruntime.Runtime, purpose, targetRef string) (func(), error) {
	if approved == nil {
		return func() {}, nil
	}
	expected, ok := approved[key(purpose, targetRef)]
	if !ok {
		return nil, ErrApprovalChanged
	}
	if runtime == nil || runtime.Storage.Database == nil {
		return nil, errors.New("database runtime is not available")
	}
	release, err := runtime.Security.VaultDelivery.AcquireDelivery(ctx)
	if err != nil {
		return nil, err
	}
	currentTarget, currentProfile, err := connectortargets.NewStore(runtime.Storage.Database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil || !reflect.DeepEqual(currentTarget, expected.Target) || !reflect.DeepEqual(currentProfile, expected.Profile) {
		release()
		return nil, ErrApprovalChanged
	}
	return release, nil
}

func key(purpose, targetRef string) string {
	return strings.TrimSpace(purpose) + "\x00" + strings.TrimSpace(targetRef)
}
