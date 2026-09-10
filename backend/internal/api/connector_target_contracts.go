package api

import "github.com/aipermission/aipermission/backend/internal/connectormanagement"

type createConnectorTargetRequest = connectormanagement.CreateTargetRequest

type createConnectorCredentialProfileRequest = connectormanagement.CredentialProfileInput

type updateConnectorTargetRequest = connectormanagement.UpdateTargetRequest

type updateConnectorCredentialProfileRequest = connectormanagement.CredentialProfileInput

type createConnectorTargetWithProfileRequest struct {
	Target  createConnectorTargetRequest            `json:"target"`
	Profile createConnectorCredentialProfileRequest `json:"profile"`
}

type updateConnectorTargetWithProfileRequest struct {
	Target  updateConnectorTargetRequest            `json:"target"`
	Profile updateConnectorCredentialProfileRequest `json:"profile"`
}

type connectorTargetTestResponse struct {
	TargetID      int64          `json:"target_id"`
	ProfileID     int64          `json:"profile_id"`
	ConnectorKind string         `json:"connector_kind"`
	OK            bool           `json:"ok"`
	Status        string         `json:"status"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	DurationMS    int64          `json:"duration_ms"`
}

type connectorTargetResponse = connectormanagement.TargetResponse
type profileSummary = connectormanagement.ProfileSummary
