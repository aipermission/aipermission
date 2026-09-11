package gatewaytransfer

import (
	"context"
	"errors"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

var errTransferExecutionStale = errors.New("file transfer credential or target changed after execution was accepted")

type transferExecution struct {
	runtimeID     int64
	connectorKind string
	target        connectors.TargetView
	profile       connectors.CredentialProfileView
	adapter       connectorapi.FileTransferAdapter
	gateway       connectorapi.FileTransferGateway
	runtime       connectorapi.TransferRuntime
	boundary      actionresult.CredentialBoundary
}

func (h FileTransferHTTPHandlers) resolveTransferExecution(ctx context.Context, runtime *transferapp.Runtime, runtimeID int64) (transferExecution, error) {
	ports, err := connectorFileTransferPortsForID(ctx, runtime, runtimeID)
	if err != nil {
		return transferExecution{}, err
	}
	adapter := h.connectorFileTransferAdapterFor(ports.ConnectorKind)
	if adapter == nil {
		return transferExecution{}, connectortargets.ErrInvalidTargetRef
	}
	target, profile, surface, err := ports.Runtime.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return transferExecution{}, err
	}
	if target.ConnectorKind != ports.ConnectorKind || profile.ConnectorKind != ports.ConnectorKind ||
		surface.ConnectorKind != ports.ConnectorKind || surface.CapabilityKind != connectortargets.RuntimeCapabilityFileTransfer {
		return transferExecution{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	execution := transferExecution{
		runtimeID:     runtimeID,
		connectorKind: ports.ConnectorKind,
		target:        target,
		profile:       profile,
		adapter:       adapter,
		gateway:       ports.Gateway,
		boundary:      ports.CredentialBoundary,
	}
	execution.runtime = boundTransferRuntime{
		delegate:  ports.Runtime,
		target:    target,
		profile:   profile,
		surface:   surface,
		resources: newCredentialResourceBindings(execution.boundary),
	}
	return execution, nil
}

func (execution transferExecution) authorizedBy(authorization connectorapi.TransferAuthorization) bool {
	return authorization.ConnectorKind == execution.connectorKind &&
		authorization.TargetID == execution.target.ID && authorization.TargetRef == execution.target.Ref && authorization.TargetUpdatedAt == execution.target.UpdatedAt &&
		authorization.ProfileID == execution.profile.ID && authorization.ProfileUpdatedAt == execution.profile.UpdatedAt &&
		authorization.ProfileSecretRevision == execution.profile.SecretRevision
}

func (execution transferExecution) runnerExecution() transferapp.Execution {
	return transferapp.Execution{
		RuntimeID: execution.runtimeID,
		Adapter:   execution.adapter, Gateway: execution.gateway, Runtime: execution.runtime,
		Boundary: execution.boundary,
	}
}

type boundTransferRuntime struct {
	delegate  connectorapi.TransferRuntime
	target    connectors.TargetView
	profile   connectors.CredentialProfileView
	surface   connectortargets.RuntimeSurface
	resources *credentialResourceBindings
}

func (r boundTransferRuntime) ResolveConnectorActionTarget(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	if strings.TrimSpace(targetRef) != r.target.Ref {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.ErrInvalidTargetRef
	}
	target, profile, err := r.delegate.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	if !r.matches(target, profile, r.surface) {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, errTransferExecutionStale
	}
	return target, profile, nil
}

func (r boundTransferRuntime) EnsureRuntimeSurface(_ context.Context, input connectortargets.EnsureRuntimeSurfaceInput) (connectortargets.RuntimeSurface, error) {
	if input.ConnectorKind != "" && input.ConnectorKind != r.surface.ConnectorKind ||
		input.TargetID != r.surface.TargetID || input.ProfileID != r.surface.ProfileID ||
		strings.TrimSpace(input.CapabilityKind) != r.surface.CapabilityKind {
		return connectortargets.RuntimeSurface{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	return r.surface, nil
}

func (r boundTransferRuntime) ListRuntimeSurfacesForProfile(_ context.Context, targetID int64, profileID int64, capabilityKind string) ([]connectortargets.RuntimeSurface, error) {
	if targetID != r.surface.TargetID || profileID != r.surface.ProfileID || strings.TrimSpace(capabilityKind) != r.surface.CapabilityKind {
		return nil, connectortargets.ErrRuntimeSurfaceNotFound
	}
	return []connectortargets.RuntimeSurface{r.surface}, nil
}

func (r boundTransferRuntime) TargetProfileByRuntimeID(ctx context.Context, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error) {
	if runtimeID != r.surface.ID {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.RuntimeSurface{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	target, profile, surface, err := r.delegate.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.RuntimeSurface{}, err
	}
	if !r.matches(target, profile, surface) {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.RuntimeSurface{}, errTransferExecutionStale
	}
	return target, profile, surface, nil
}

func (r boundTransferRuntime) ListCredentialProfiles(ctx context.Context, targetID int64) ([]connectors.CredentialProfileView, error) {
	if targetID != r.profile.TargetID {
		return nil, connectortargets.ErrTargetNotFound
	}
	profiles, err := r.delegate.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.ID == r.profile.ID {
			if !sameTransferProfile(profile, r.profile) {
				return nil, errTransferExecutionStale
			}
			return []connectors.CredentialProfileView{profile}, nil
		}
	}
	return nil, errTransferExecutionStale
}

func (r boundTransferRuntime) CredentialResources(resourceKind string) connectorapi.CredentialResourceStore {
	delegate := r.delegate.CredentialResources(resourceKind)
	if delegate == nil || r.resources == nil {
		return nil
	}
	return boundCredentialResourceStore{
		delegate: delegate,
		kind:     strings.TrimSpace(resourceKind),
		bindings: r.resources,
	}
}

func (r boundTransferRuntime) ResolveRuntimeContext(ctx context.Context, runtimeID int64, capabilityKind string) (connectors.RuntimeContext, connectortargets.RuntimeSurface, error) {
	if runtimeID != r.surface.ID || strings.TrimSpace(capabilityKind) != r.surface.CapabilityKind {
		return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	contextValue, surface, err := r.delegate.ResolveRuntimeContext(ctx, runtimeID, capabilityKind)
	if err != nil {
		return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, err
	}
	if !r.matches(contextValue.Target, contextValue.Profile, surface) {
		return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, errTransferExecutionStale
	}
	return contextValue, surface, nil
}

func (r boundTransferRuntime) matches(target connectors.TargetView, profile connectors.CredentialProfileView, surface connectortargets.RuntimeSurface) bool {
	return target.ID == r.target.ID && target.Ref == r.target.Ref && target.ConnectorKind == r.target.ConnectorKind && target.UpdatedAt == r.target.UpdatedAt &&
		sameTransferProfile(profile, r.profile) &&
		surface.ID == r.surface.ID && surface.TargetID == r.surface.TargetID && surface.ProfileID == r.surface.ProfileID &&
		surface.ConnectorKind == r.surface.ConnectorKind && surface.CapabilityKind == r.surface.CapabilityKind &&
		surface.Status == r.surface.Status && surface.UpdatedAt == r.surface.UpdatedAt
}

func sameTransferProfile(left connectors.CredentialProfileView, right connectors.CredentialProfileView) bool {
	return left.ID == right.ID && left.TargetID == right.TargetID && left.ConnectorKind == right.ConnectorKind &&
		left.UpdatedAt == right.UpdatedAt && left.SecretRevision == right.SecretRevision
}

var _ connectorapi.TransferRuntime = boundTransferRuntime{}
