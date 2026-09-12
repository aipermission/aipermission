package connectorruntime

import (
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func gatewayRuntimeSurface(surface connectortargets.RuntimeSurface) connectorapi.RuntimeSurface {
	return connectorapi.RuntimeSurface{
		ID: surface.ID, ConnectorKind: surface.ConnectorKind, TargetID: surface.TargetID,
		ProfileID: surface.ProfileID, CapabilityKind: surface.CapabilityKind, Label: surface.Label,
		Status: string(surface.Status), CreatedAt: surface.CreatedAt, UpdatedAt: surface.UpdatedAt,
	}
}

func gatewayRuntimeSurfaces(surfaces []connectortargets.RuntimeSurface) []connectorapi.RuntimeSurface {
	result := make([]connectorapi.RuntimeSurface, 0, len(surfaces))
	for _, surface := range surfaces {
		result = append(result, gatewayRuntimeSurface(surface))
	}
	return result
}

func coreRuntimeSurfaceInput(input connectorapi.EnsureRuntimeSurfaceInput) connectortargets.EnsureRuntimeSurfaceInput {
	return connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: input.ConnectorKind, TargetID: input.TargetID, ProfileID: input.ProfileID,
		CapabilityKind: input.CapabilityKind, Label: input.Label,
	}
}

func corePrincipal(principal connectorapi.Principal) (executionprincipal.Principal, error) {
	if err := principal.Validate(); err != nil {
		return executionprincipal.Principal{}, executionprincipal.ErrInvalid
	}
	return executionprincipal.Principal{
		Kind: executionprincipal.Kind(principal.Kind), TokenID: principal.TokenID,
		WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
	}, nil
}

func gatewayPrincipal(principal executionprincipal.Principal) connectorapi.Principal {
	return connectorapi.Principal{
		Kind: connectorapi.PrincipalKind(principal.Kind), TokenID: principal.TokenID,
		WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
	}
}

func gatewaySessionHandle(handle console.SessionHandle) connectorapi.ConsoleSessionHandle {
	return connectorapi.ConsoleSessionHandle{ID: handle.ID, RuntimeID: handle.RuntimeID, Generation: handle.Generation}
}

func coreSessionHandle(handle connectorapi.ConsoleSessionHandle) console.SessionHandle {
	return console.SessionHandle{ID: handle.ID, RuntimeID: handle.RuntimeID, Generation: handle.Generation}
}

func gatewayExecResult(result console.ExecResult) connectorapi.ConsoleExecResult {
	return connectorapi.ConsoleExecResult{
		SessionID: result.SessionID, Generation: result.Generation, Command: result.Command,
		Output: result.Output, ExitCode: result.ExitCode, Running: result.Running, DurationMS: result.DurationMS,
	}
}

func gatewayConsoleRecord(record console.Record) connectorapi.ConsoleRecord {
	return connectorapi.ConsoleRecord{
		ID: record.ID, RuntimeID: record.RuntimeID, Generation: record.Generation,
		TargetName: record.TargetName, Name: record.Name, Status: record.Status,
		Transcript: record.Transcript, Error: record.Error, Cols: record.Cols, Rows: record.Rows,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, ClosedAt: record.ClosedAt,
		EnvironmentContentHash: record.EnvironmentContentHash,
	}
}
