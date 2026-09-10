package api

import "github.com/aipermission/aipermission/backend/internal/connectormanagement"

type createConnectorTargetRequest = connectormanagement.CreateTargetRequest

type createConnectorCredentialProfileRequest struct {
	Kind      string         `json:"kind"`
	Label     string         `json:"label"`
	Public    map[string]any `json:"public,omitempty"`
	Secret    map[string]any `json:"secret,omitempty"`
	RiskLabel string         `json:"risk_label,omitempty"`
}

type updateConnectorTargetRequest = connectormanagement.UpdateTargetRequest

type updateConnectorCredentialProfileRequest struct {
	Kind      string         `json:"kind"`
	Label     string         `json:"label"`
	Public    map[string]any `json:"public,omitempty"`
	Secret    map[string]any `json:"secret,omitempty"`
	RiskLabel string         `json:"risk_label,omitempty"`
}

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
