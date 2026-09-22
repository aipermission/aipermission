// Package securitypolicy owns workspace security settings and persistence
// redaction policy.
package securitypolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	RedactionModeOff   = "off"
	RedactionModeBasic = "basic"
)

type Settings struct {
	ReusableTokens          bool   `json:"reusable_tokens"`
	ExposeMCPServerMetadata bool   `json:"expose_mcp_server_metadata"`
	MCPStartEnabled         bool   `json:"mcp_start_enabled"`
	RedactionMode           string `json:"redaction_mode"`
}

type SettingsDocument struct {
	Settings
	Revision string `json:"revision"`
}

type SettingsUpdateRequest struct {
	ReusableTokens          *bool   `json:"reusable_tokens"`
	ExposeMCPServerMetadata *bool   `json:"expose_mcp_server_metadata"`
	MCPStartEnabled         *bool   `json:"mcp_start_enabled"`
	RedactionMode           *string `json:"redaction_mode"`
	ExpectedRevision        string  `json:"expected_revision"`
	Revision                string  `json:"revision"`
}

func (request SettingsUpdateRequest) RevisionValue() (string, error) {
	expected := strings.TrimSpace(request.ExpectedRevision)
	legacy := strings.TrimSpace(request.Revision)
	if expected != "" && legacy != "" && !strings.EqualFold(expected, legacy) {
		return "", ValidationError("security settings revision fields conflict")
	}
	if expected != "" {
		return expected, nil
	}
	return legacy, nil
}

func NewSettingsUpdateRequest(settings Settings, expectedRevision string) SettingsUpdateRequest {
	return SettingsUpdateRequest{
		ReusableTokens:          &settings.ReusableTokens,
		ExposeMCPServerMetadata: &settings.ExposeMCPServerMetadata,
		MCPStartEnabled:         &settings.MCPStartEnabled,
		RedactionMode:           &settings.RedactionMode,
		ExpectedRevision:        expectedRevision,
	}
}

func (request SettingsUpdateRequest) SettingsValue() (Settings, error) {
	if request.ReusableTokens == nil || request.ExposeMCPServerMetadata == nil ||
		request.MCPStartEnabled == nil || request.RedactionMode == nil {
		return Settings{}, ValidationError("all security settings are required")
	}
	return Settings{
		ReusableTokens:          *request.ReusableTokens,
		ExposeMCPServerMetadata: *request.ExposeMCPServerMetadata,
		MCPStartEnabled:         *request.MCPStartEnabled,
		RedactionMode:           *request.RedactionMode,
	}, nil
}

var (
	ErrSettingsRevisionRequired = errors.New("security settings revision is required")
	ErrSettingsRevisionConflict = errors.New("security settings changed since they were loaded")
)

func NewSettingsDocument(settings Settings) (SettingsDocument, error) {
	settings = NormalizeSettings(settings)
	revision, err := settingsRevision(settings)
	if err != nil {
		return SettingsDocument{}, err
	}
	return SettingsDocument{
		Settings: settings,
		Revision: revision,
	}, nil
}

func settingsRevision(settings Settings) (string, error) {
	payload, err := json.Marshal(NormalizeSettings(settings))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func requireSettingsRevision(expected string, settings Settings) error {
	if strings.TrimSpace(expected) == "" {
		return ErrSettingsRevisionRequired
	}
	current, err := settingsRevision(settings)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(expected), current) {
		return ErrSettingsRevisionConflict
	}
	return nil
}

func NormalizeSettings(settings Settings) Settings {
	settings.RedactionMode = NormalizeRedactionMode(settings.RedactionMode)
	return settings
}

func NormalizeRedactionMode(value string) string {
	if value == RedactionModeOff {
		return RedactionModeOff
	}
	return RedactionModeBasic
}

type Rule struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Pattern   string `json:"pattern"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type RuleInput struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Enabled bool   `json:"enabled"`
}

type ValidationError string

func (e ValidationError) Error() string { return string(e) }
