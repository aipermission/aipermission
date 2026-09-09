// Package securitypolicy owns workspace security settings and persistence
// redaction policy.
package securitypolicy

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
