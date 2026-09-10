package projectvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

const gatewaySecretSetting = "gateway_secret"

// ResolveGatewaySecret returns the workspace-bound secret. A configuration
// fallback may bootstrap an unbound database, but must never replace missing
// identity material after encrypted record envelopes exist.
func ResolveGatewaySecret(
	ctx context.Context,
	database *sql.DB,
	fallback string,
) (string, error) {
	if database == nil {
		return "", errors.New("workspace database is unavailable")
	}
	var stored string
	err := database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, gatewaySecretSetting).Scan(&stored)
	if err == nil && strings.TrimSpace(stored) != "" {
		return strings.TrimSpace(stored), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read gateway secret setting: %w", err)
	}
	envelopeBound, markerErr := recordcrypto.EnvelopeMarkerPresent(ctx, database)
	if markerErr != nil {
		return "", markerErr
	}
	if envelopeBound {
		return "", errors.New("gateway secret is missing from an envelope-bound database")
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return "", errors.New("gateway secret is missing")
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		gatewaySecretSetting, fallback, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return "", fmt.Errorf("write gateway secret setting: %w", err)
	}
	return fallback, nil
}
