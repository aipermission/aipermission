package api

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

var errConnectorTransportApprovalChanged = errors.New("connector transport approval dependency changed before use")

type approvedConnectorTransports map[string]actions.ResolvedDependency

func newApprovedConnectorTransports(dependencies []actions.ResolvedDependency) approvedConnectorTransports {
	approved := make(approvedConnectorTransports, len(dependencies))
	for _, dependency := range dependencies {
		approved[connectorTransportApprovalKey(dependency.Purpose, dependency.Target.Ref)] = dependency
	}
	return approved
}

func (approved approvedConnectorTransports) acquire(
	ctx context.Context,
	runtime *databaseRuntime,
	purpose string,
	targetRef string,
) (func(), error) {
	if approved == nil {
		return func() {}, nil
	}
	expected, ok := approved[connectorTransportApprovalKey(purpose, targetRef)]
	if !ok {
		return nil, errConnectorTransportApprovalChanged
	}
	if runtime == nil || runtime.database == nil {
		return nil, errors.New("database runtime is not available")
	}
	release, err := runtime.vaultDelivery.acquire(ctx)
	if err != nil {
		return nil, err
	}
	currentTarget, currentProfile, err := connectortargets.NewStore(runtime.database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil || !reflect.DeepEqual(currentTarget, expected.Target) || !reflect.DeepEqual(currentProfile, expected.Profile) {
		release()
		return nil, errConnectorTransportApprovalChanged
	}
	return release, nil
}

func connectorTransportApprovalKey(purpose, targetRef string) string {
	return strings.TrimSpace(purpose) + "\x00" + strings.TrimSpace(targetRef)
}
