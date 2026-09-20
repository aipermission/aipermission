package servicebaseline

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

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const settingPrefix = "backup_service_baseline_"

type Baseline struct {
	BackupID  string `json:"backup_id"`
	CreatedAt string `json:"created_at"`
}

func Read(ctx context.Context, db sqldb.Executor, baseURL, streamID string, validID func(string) bool) (*Baseline, error) {
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key(baseURL, streamID)).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read backup service baseline: %w", err)
	}
	var baseline Baseline
	if err := json.Unmarshal([]byte(raw), &baseline); err != nil {
		return nil, fmt.Errorf("decode backup service baseline: %w", err)
	}
	if err := validate(baseline, validID); err != nil {
		return nil, err
	}
	return &baseline, nil
}

func Write(ctx context.Context, db sqldb.Executor, baseURL, streamID string, baseline Baseline, validID func(string) bool) error {
	if err := validate(baseline, validID); err != nil {
		return err
	}
	raw, err := json.Marshal(baseline)
	if err != nil {
		return fmt.Errorf("encode backup service baseline: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO settings(key, value, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key(baseURL, streamID), string(raw), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("write backup service baseline: %w", err)
	}
	return nil
}

func key(baseURL, streamID string) string {
	digest := sha256.Sum256([]byte(baseURL + "\x00" + streamID))
	return settingPrefix + hex.EncodeToString(digest[:16])
}

func validate(baseline Baseline, validID func(string) bool) error {
	if validID == nil || !validID(strings.TrimSpace(baseline.BackupID)) {
		return errors.New("backup service baseline contains an invalid version id")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(baseline.CreatedAt))
	if err != nil || createdAt.IsZero() {
		return errors.New("backup service baseline contains an invalid creation time")
	}
	return nil
}
