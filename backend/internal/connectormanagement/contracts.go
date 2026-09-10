package connectormanagement

import "github.com/aipermission/aipermission/backend/internal/connectors"

type CreateTargetRequest struct {
	ProjectID     int64          `json:"project_id"`
	ConnectorKind string         `json:"connector_kind"`
	Name          string         `json:"name"`
	Config        map[string]any `json:"config,omitempty"`
	Profile       map[string]any `json:"profile,omitempty"`
}

type UpdateTargetRequest struct {
	ProjectID int64          `json:"project_id"`
	Name      string         `json:"name"`
	Config    map[string]any `json:"config,omitempty"`
}

type TargetResponse struct {
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
	Profiles      []ProfileSummary `json:"profiles,omitempty"`
}

type ProfileSummary struct {
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

type TargetProfileItem struct {
	Ref               string         `json:"ref"`
	ProjectID         int64          `json:"project_id"`
	ProjectName       string         `json:"project_name"`
	ProjectSlug       string         `json:"project_slug"`
	ConnectorKind     string         `json:"connector_kind"`
	TargetID          int64          `json:"target_id"`
	TargetName        string         `json:"target_name"`
	ProfileID         int64          `json:"profile_id"`
	ProfileKind       string         `json:"profile_kind"`
	ProfileLabel      string         `json:"profile_label"`
	RuntimeID         int64          `json:"runtime_id,omitempty"`
	TransferRuntimeID int64          `json:"transfer_runtime_id,omitempty"`
	Config            map[string]any `json:"config,omitempty"`
	Public            map[string]any `json:"public,omitempty"`
	Status            string         `json:"status"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}
