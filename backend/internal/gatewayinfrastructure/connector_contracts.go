package gatewayinfrastructure

import (
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func connectorPrincipal(principal gatewayaccess.Principal) connectorapi.Principal {
	return connectorapi.Principal{
		Kind: connectorapi.PrincipalKind(principal.Kind), TokenID: principal.TokenID,
		WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
	}
}

func connectorActionRequest(request connectormgmt.ActionRequest) connectorapi.ActionRequest {
	return connectorapi.ActionRequest{
		ID: request.ID, ConnectorKind: request.ConnectorKind, ActionName: request.ActionName, Status: request.Status,
	}
}

func connectorTarget(target connectormgmt.Target) connectorapi.Target {
	return connectorapi.Target{
		ID: target.ID, ProjectID: target.ProjectID, ProjectName: target.ProjectName, ProjectSlug: target.ProjectSlug,
		ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config,
		Status: string(target.Status), CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt,
	}
}

func connectorCredentialProfile(profile connectormgmt.CredentialProfile) connectorapi.CredentialProfile {
	return connectorapi.CredentialProfile{
		ID: profile.ID, TargetID: profile.TargetID, ConnectorKind: profile.ConnectorKind,
		Kind: profile.Kind, Label: profile.Label, Public: profile.Public,
		EncryptedSecretJSON: profile.EncryptedSecretJSON, RiskLabel: profile.RiskLabel,
		SecretRevision: profile.SecretRevision, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
	}
}
