package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	reusableTokensSettingKey   = "reusable_tokens_enabled"
	exposeMCPServerMetadataKey = "expose_mcp_server_metadata"
	mcpStartEnabledSettingKey  = "mcp_start_enabled"
	redactionModeSettingKey    = "redaction_mode"
	defaultRedactionMode       = "basic"
	redactionModeOff           = "off"
	redactionModeBasic         = "basic"
)

type securitySettingsResponse struct {
	ReusableTokens          bool   `json:"reusable_tokens"`
	ExposeMCPServerMetadata bool   `json:"expose_mcp_server_metadata"`
	MCPStartEnabled         bool   `json:"mcp_start_enabled"`
	RedactionMode           string `json:"redaction_mode"`
}

type updateSecuritySettingsRequest struct {
	ReusableTokens          bool   `json:"reusable_tokens"`
	ExposeMCPServerMetadata bool   `json:"expose_mcp_server_metadata"`
	MCPStartEnabled         bool   `json:"mcp_start_enabled"`
	RedactionMode           string `json:"redaction_mode"`
}

func (s securityHandlers) getSecuritySettings(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	settings, err := readSecuritySettings(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s securityHandlers) updateSecuritySettings(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request updateSecuritySettingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	settings := securitySettingsResponse{
		ReusableTokens:          request.ReusableTokens,
		ExposeMCPServerMetadata: request.ExposeMCPServerMetadata,
		MCPStartEnabled:         request.MCPStartEnabled,
		RedactionMode:           normalizeRedactionMode(request.RedactionMode),
	}
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "settings.security.updated",
		func() any { return securitySettingsAuditPayload(settings) },
		func(tx *sql.Tx) error { return writeSecuritySettingsWithExecutor(r.Context(), tx, settings) },
	)
	if err != nil {
		writeInternalError(w)
		return
	}
	runtime.securityMu.Lock()
	runtime.securitySettings = settings
	runtime.securityLoaded = true
	runtime.securityMu.Unlock()
	writeJSON(w, http.StatusOK, settings)
}

func securitySettingsAuditPayload(settings securitySettingsResponse) map[string]any {
	return map[string]any{
		"reusable_tokens":            settings.ReusableTokens,
		"expose_mcp_server_metadata": settings.ExposeMCPServerMetadata,
		"mcp_start_enabled":          settings.MCPStartEnabled,
		"redaction_mode":             settings.RedactionMode,
	}
}

func readSecuritySettings(ctx context.Context, runtime *databaseRuntime) (securitySettingsResponse, error) {
	runtime.securityMu.RLock()
	if runtime.securityLoaded {
		settings := runtime.securitySettings
		runtime.securityMu.RUnlock()
		return settings, nil
	}
	runtime.securityMu.RUnlock()

	runtime.securityMu.Lock()
	defer runtime.securityMu.Unlock()
	if runtime.securityLoaded {
		return runtime.securitySettings, nil
	}
	settings, err := readSecuritySettingsFromDB(ctx, runtime)
	if err != nil {
		return securitySettingsResponse{}, err
	}
	runtime.securitySettings = settings
	runtime.securityLoaded = true
	return settings, nil
}

func readSecuritySettingsFromDB(ctx context.Context, runtime *databaseRuntime) (securitySettingsResponse, error) {
	values, err := readSettingsMap(ctx, runtime, reusableTokensSettingKey, exposeMCPServerMetadataKey, mcpStartEnabledSettingKey, redactionModeSettingKey)
	if err != nil {
		return securitySettingsResponse{}, err
	}
	return securitySettingsResponse{
		ReusableTokens:          values[reusableTokensSettingKey] == "true",
		ExposeMCPServerMetadata: values[exposeMCPServerMetadataKey] == "true",
		MCPStartEnabled:         values[mcpStartEnabledSettingKey] == "true",
		RedactionMode:           normalizeRedactionMode(values[redactionModeSettingKey]),
	}, nil
}

func writeSecuritySettings(ctx context.Context, runtime *databaseRuntime, settings securitySettingsResponse) error {
	tx, err := runtime.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := writeSecuritySettingsWithExecutor(ctx, tx, settings); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	runtime.securityMu.Lock()
	runtime.securitySettings = settings
	runtime.securityLoaded = true
	runtime.securityMu.Unlock()
	return nil
}

func writeSecuritySettingsWithExecutor(ctx context.Context, executor sqldb.Executor, settings securitySettingsResponse) error {
	now := time.Now().UTC().Format(time.RFC3339)
	reusableValue := "false"
	if settings.ReusableTokens {
		reusableValue = "true"
	}
	metadataValue := "false"
	if settings.ExposeMCPServerMetadata {
		metadataValue = "true"
	}
	mcpStartValue := "false"
	if settings.MCPStartEnabled {
		mcpStartValue = "true"
	}
	for key, value := range map[string]string{
		reusableTokensSettingKey:   reusableValue,
		exposeMCPServerMetadataKey: metadataValue,
		mcpStartEnabledSettingKey:  mcpStartValue,
		redactionModeSettingKey:    normalizeRedactionMode(settings.RedactionMode),
	} {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			key,
			value,
			now,
		); err != nil {
			return err
		}
	}
	if !settings.ReusableTokens {
		if _, err := executor.ExecContext(ctx, `UPDATE api_tokens SET token_value = '', updated_at = ?`, now); err != nil {
			return err
		}
	}
	return nil
}

func readSettingsMap(ctx context.Context, runtime *databaseRuntime, keys ...string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		var value string
		err := runtime.database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func normalizeRedactionMode(value string) string {
	switch value {
	case redactionModeOff, redactionModeBasic:
		return value
	default:
		return defaultRedactionMode
	}
}

func stripReusableTokenValues(items []tokens.Token) []tokens.Token {
	for i := range items {
		items[i].TokenValue = ""
	}
	return items
}
