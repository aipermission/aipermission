package api

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type vaultGenerateActionInput struct {
	Name              string                      `json:"name"`
	SecretType        string                      `json:"secret_type,omitempty"`
	GeneratorKind     string                      `json:"generator_kind"`
	Provider          string                      `json:"provider,omitempty"`
	Environment       string                      `json:"environment,omitempty"`
	Description       string                      `json:"description,omitempty"`
	ExpiresAt         string                      `json:"expires_at,omitempty"`
	ExpiryWarningDays int                         `json:"expiry_warning_days,omitempty"`
	Tags              []string                    `json:"tags,omitempty"`
	UsageNotes        []vaultActionUsageNoteInput `json:"usage_notes,omitempty"`
	SharedProjectIDs  []int64                     `json:"shared_project_ids,omitempty"`
}

type vaultActionUsageNoteInput struct {
	Location string `json:"location"`
	Notes    string `json:"notes,omitempty"`
}

type vaultSessionApplyActionInput struct {
	TargetRef string                             `json:"target_ref"`
	Items     []vaultActionSessionSelectionInput `json:"items"`
}

type vaultActionSessionSelectionInput struct {
	ItemID          int64 `json:"item_id"`
	SourceProjectID int64 `json:"source_project_id"`
	ReplaceExisting bool  `json:"replace_existing,omitempty"`
}

func (input vaultGenerateActionInput) projectUsageNotes() []projectvault.UsageNote {
	items := make([]projectvault.UsageNote, 0, len(input.UsageNotes))
	for _, note := range input.UsageNotes {
		items = append(items, projectvault.UsageNote{Location: note.Location, Notes: note.Notes})
	}
	return items
}

func (input vaultSessionApplyActionInput) sessionSelections() []projectvault.SessionSelection {
	items := make([]projectvault.SessionSelection, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, projectvault.SessionSelection{
			ItemID: item.ItemID, SourceProjectID: item.SourceProjectID,
			ReplaceExisting: item.ReplaceExisting,
		})
	}
	return items
}

func normalizeVaultActionInput(actionName string, input map[string]any) (map[string]any, error) {
	var typed any
	switch actionName {
	case vaultrequests.ActionGenerateItem:
		typed = &vaultGenerateActionInput{}
	case vaultrequests.ActionRestartSession:
		typed = &vaultSessionApplyActionInput{}
	default:
		return nil, errors.New("unsupported Vault action")
	}
	if err := decodeMap(input, typed); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	normalized := map[string]any{}
	if err := json.Unmarshal(payload, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func decodeMap(input map[string]any, target any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
