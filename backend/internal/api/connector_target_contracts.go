package api

import "github.com/aipermission/aipermission/backend/internal/connectors"

type createConnectorTargetRequest struct {
	ProjectID     int64          `json:"project_id"`
	ConnectorKind string         `json:"connector_kind"`
	Name          string         `json:"name"`
	Config        map[string]any `json:"config,omitempty"`
	Profile       map[string]any `json:"profile,omitempty"`
}

type createConnectorCredentialProfileRequest struct {
	Kind      string         `json:"kind"`
	Label     string         `json:"label"`
	Public    map[string]any `json:"public,omitempty"`
	Secret    map[string]any `json:"secret,omitempty"`
	RiskLabel string         `json:"risk_label,omitempty"`
}

type updateConnectorTargetRequest struct {
	ProjectID int64          `json:"project_id"`
	Name      string         `json:"name"`
	Config    map[string]any `json:"config,omitempty"`
}

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

type connectorTargetResponse struct {
	ID            int64            `json:"id"`
	ProjectID     int64            `json:"project_id"`
	ProjectName   string           `json:"project_name"`
	ProjectSlug   string           `json:"project_slug"`
	Ref           string           `json:"ref,omitempty"`
	ConnectorKind string           `json:"connector_kind"`
	Name          string           `json:"name"`
	Config        map[string]any   `json:"config,omitempty"`
	Status        string           `json:"status"`
	CreatedAt     string           `json:"created_at"`
	UpdatedAt     string           `json:"updated_at"`
	Profiles      []profileSummary `json:"profiles,omitempty"`
}

type profileSummary struct {
	ID                int64                         `json:"id"`
	TargetID          int64                         `json:"target_id"`
	Ref               string                        `json:"ref"`
	ConnectorKind     string                        `json:"connector_kind"`
	Kind              string                        `json:"kind"`
	Label             string                        `json:"label"`
	Public            map[string]any                `json:"public,omitempty"`
	RiskLabel         string                        `json:"risk_label,omitempty"`
	Actions           []connectors.ActionDefinition `json:"actions,omitempty"`
	RuntimeID         int64                         `json:"runtime_id,omitempty"`
	TransferRuntimeID int64                         `json:"transfer_runtime_id,omitempty"`
	VaultSession      bool                          `json:"vault_session_supported"`
	CreatedAt         string                        `json:"created_at"`
	UpdatedAt         string                        `json:"updated_at"`
}
