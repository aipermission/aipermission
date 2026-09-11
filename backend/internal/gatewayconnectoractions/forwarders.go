package gatewayconnectoractions

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	applicationactions "github.com/aipermission/aipermission/backend/internal/applicationconnectoractions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func NewCredentialBoundary(secrets map[string]any) actions.CredentialBoundary {
	return actions.NewCredentialBoundary(secrets)
}

func SensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return actions.SensitiveOutputFields(hints...)
}

func Delivery(runtime *workspaceruntime.Runtime) actions.DeliveryGate {
	return applicationactions.Delivery(runtime)
}

func New(dependencies applicationactions.Dependencies) *applicationactions.Component {
	return applicationactions.New(dependencies)
}

func Prepare(runtime *workspaceruntime.Runtime, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	return applicationactions.Prepare(runtime, ctx, request)
}

func StopRecovery(runtime *workspaceruntime.Runtime) {
	applicationactions.StopRecovery(runtime)
}
