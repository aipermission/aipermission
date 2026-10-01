package connectorcredentials

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

// ReadCompletedCleanupEvidence runs only under the caller's exclusive lifecycle
// admission. A nil result without error requires normal authenticated cleanup.
func (ports RuntimePorts) ReadCompletedCleanupEvidence(ctx context.Context, capabilitiesFor func(string) (connectors.RuntimeCapabilityResolver, error), connector connectors.Connector, target connectortargets.Target, profile connectortargets.CredentialProfile) (*connectors.ActionResult, bool, error) {
	reader, ok := connector.(connectors.ProvisionedCredentialCleanupEvidence)
	if !ok {
		return nil, false, nil
	}
	if ctx == nil || capabilitiesFor == nil || !ports.Valid() || target.ID < 1 || profile.ID < 1 ||
		profile.TargetID != target.ID || target.ConnectorKind == "" || profile.ConnectorKind != target.ConnectorKind {
		return nil, true, errors.New("connector cleanup evidence runtime is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	capabilities, err := capabilitiesFor(target.ConnectorKind)
	if err != nil {
		return nil, true, errors.New(ports.RedactText(ctx, err.Error()))
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	if resourcecontract.IsNilDependency(capabilities) {
		return nil, true, errors.New("connector cleanup evidence runtime is unavailable")
	}
	result, err := reader.ReadCompletedCredentialCleanup(ctx, connectors.CleanupEvidenceContext{
		Target: TargetView(target, profile.ID), Capabilities: capabilities,
	}, connectortargets.CredentialProfileView(profile))
	if canceled := ctx.Err(); canceled != nil {
		return nil, true, canceled
	}
	if err != nil {
		return nil, true, errors.New(ports.RedactText(ctx, err.Error()))
	}
	if result == nil {
		return nil, false, nil
	}
	projected, err := ports.ProjectCompletedCleanup(ctx, *result, nil, actionresult.CombinedCredentialBoundary())
	if err != nil {
		return nil, true, err
	}
	return &projected, true, nil
}

func RequireCompletedCleanup(result connectors.ActionResult, err error) error {
	if err != nil {
		return err
	}
	if result.Status != connectors.ResultCompleted {
		return fmt.Errorf("credential cleanup returned status %q", result.Status)
	}
	if result.Error != "" || result.Handles != (connectors.ActionHandles{}) {
		return errors.New("credential cleanup must be a terminal confirmation without errors or follow-up handles")
	}
	return nil
}

func (ports RuntimePorts) ProjectCompletedCleanup(ctx context.Context, result connectors.ActionResult, cleanupErr error, boundary actionresult.CredentialBoundary) (connectors.ActionResult, error) {
	if ctx == nil || !ports.Valid() {
		return connectors.ActionResult{}, errors.New("credential cleanup projection is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return connectors.ActionResult{}, err
	}
	if err := RequireCompletedCleanup(result, cleanupErr); err != nil {
		return connectors.ActionResult{}, errors.New(ports.RedactCredentialText(ctx, err.Error(), boundary))
	}
	redacted, err := ports.RedactResult(ctx, result, boundary)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("process credential cleanup result: %s", ports.RedactCredentialText(ctx, err.Error(), boundary))
	}
	if err := ctx.Err(); err != nil {
		return connectors.ActionResult{}, err
	}
	if err := RequireCompletedCleanup(redacted, nil); err != nil {
		return connectors.ActionResult{}, errors.New(ports.RedactCredentialText(ctx, err.Error(), boundary))
	}
	return redacted, nil
}
