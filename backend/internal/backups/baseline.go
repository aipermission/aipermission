package backups

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const serviceBaselineSettingPrefix = "backup_service_baseline_"

type ServiceBaseline struct {
	BackupID  string `json:"backup_id"`
	CreatedAt string `json:"created_at"`
}

func ReadServiceBaseline(ctx context.Context, db storeDB, baseURL, streamID string) (*ServiceBaseline, error) {
	key, err := serviceBaselineKey(baseURL, streamID)
	if err != nil {
		return nil, err
	}
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read backup service baseline: %w", err)
	}
	var baseline ServiceBaseline
	if err := json.Unmarshal([]byte(raw), &baseline); err != nil {
		return nil, fmt.Errorf("decode backup service baseline: %w", err)
	}
	if err := validateServiceBaseline(baseline); err != nil {
		return nil, err
	}
	return &baseline, nil
}

func WriteServiceBaseline(ctx context.Context, db storeDB, baseURL, streamID string, backup ServiceBackup) error {
	key, err := serviceBaselineKey(baseURL, streamID)
	if err != nil {
		return err
	}
	baseline := ServiceBaseline{BackupID: strings.TrimSpace(backup.ID), CreatedAt: strings.TrimSpace(backup.CreatedAt)}
	if err := validateServiceBaseline(baseline); err != nil {
		return err
	}
	raw, err := json.Marshal(baseline)
	if err != nil {
		return fmt.Errorf("encode backup service baseline: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `
INSERT INTO settings(key, value, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, string(raw), now); err != nil {
		return fmt.Errorf("write backup service baseline: %w", err)
	}
	return nil
}

func serviceBaselineKey(baseURL, streamID string) (string, error) {
	normalizedURL, err := ValidateServiceURL(baseURL)
	if err != nil {
		return "", err
	}
	streamID = strings.TrimSpace(streamID)
	if !validServiceIdentifier(streamID) {
		return "", ValidationError("backup stream id is invalid")
	}
	digest := sha256.Sum256([]byte(normalizedURL + "\x00" + streamID))
	return serviceBaselineSettingPrefix + hex.EncodeToString(digest[:16]), nil
}

func validateServiceBaseline(baseline ServiceBaseline) error {
	if !validServiceIdentifier(strings.TrimSpace(baseline.BackupID)) {
		return errors.New("backup service baseline contains an invalid version id")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(baseline.CreatedAt))
	if err != nil || createdAt.IsZero() {
		return errors.New("backup service baseline contains an invalid creation time")
	}
	return nil
}
