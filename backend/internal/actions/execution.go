package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// ExecutionRequest contains the prepared request and its already-authorized,
// secret-aware runtime boundary. Approval and persistence remain gateway
// responsibilities; connector dispatch and result-contract enforcement belong
// to this service.
type ExecutionRequest struct {
	Prepared PreparedRequest
	Runtime  connectors.RuntimeContext
}

// Execute dispatches one prepared action through the connector registry and
// rejects malformed connector results before they reach persistence.
func (s *Service) Execute(ctx context.Context, request ExecutionRequest) (connectors.ActionResult, error) {
	if s == nil || s.registry == nil {
		return connectors.ActionResult{}, fmt.Errorf("action service is not configured")
	}
	if err := request.Runtime.Principal.Validate(); err != nil {
		return connectors.ActionResult{}, err
	}
	connector, ok := s.registry.Get(request.Prepared.Target.ConnectorKind)
	if !ok {
		return connectors.ActionResult{}, fmt.Errorf("connector not found: %s", request.Prepared.Target.ConnectorKind)
	}
	result, err := connector.ExecuteAction(ctx, request.Runtime, request.Prepared.Action)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if err := validateExecutionResult(result); err != nil {
		return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown("response_validation", nil, err)
	}
	return result, nil
}

func validateExecutionResult(result connectors.ActionResult) error {
	switch result.Status {
	case connectors.ResultCompleted, connectors.ResultFailed, connectors.ResultError,
		connectors.ResultOutcomeUnknown, connectors.ResultRunning:
	default:
		return fmt.Errorf("connector returned invalid action status %q", result.Status)
	}
	hasSessionID := result.Handles.SessionID > 0
	hasGeneration := result.Handles.SessionGeneration > 0
	if hasSessionID != hasGeneration {
		return errors.New("connector returned an incomplete session handle")
	}
	return nil
}
