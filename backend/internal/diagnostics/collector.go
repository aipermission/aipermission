package diagnostics

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const recentErrorReadLimit = 200
const recentErrorOutputLimit = 20

type CollectInput struct {
	Database               *sql.DB
	Registry               *connectors.Registry
	ApplicationVersion     string
	SupportedSchemaVersion int
	MCPEnabled             bool
	Audit                  AuditHealth
	Now                    func() time.Time
}

func Collect(ctx context.Context, input CollectInput) (Report, error) {
	if input.Database == nil {
		return Report{}, fmt.Errorf("diagnostics database is unavailable")
	}
	if input.Registry == nil {
		return Report{}, fmt.Errorf("diagnostics connector registry is unavailable")
	}
	now := time.Now
	if input.Now != nil {
		now = input.Now
	}
	database, err := collectDatabaseInfo(ctx, input.Database, input.SupportedSchemaVersion)
	if err != nil {
		return Report{}, err
	}
	runtimeInfo, outcomes, err := collectRuntimeInfo(ctx, input.Database, input.MCPEnabled, input.Audit)
	if err != nil {
		return Report{}, err
	}
	recentErrors, err := collectRecentErrors(ctx, input.Database)
	if err != nil {
		return Report{}, err
	}

	connectorInfos := input.Registry.List()
	connectorVersions := make([]ConnectorInfo, 0, len(connectorInfos))
	for _, info := range connectorInfos {
		connectorVersions = append(connectorVersions, ConnectorInfo{Kind: safeIdentifier(info.Kind), Version: safeVersion(info.Version)})
	}

	return Report{
		ReportFormatVersion: ReportFormatVersion,
		GeneratedAt:         now().UTC().Format(time.RFC3339),
		Application:         ApplicationInfo{Service: "aipermission", Version: safeVersion(input.ApplicationVersion)},
		Architecture: ArchitectureInfo{
			OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(),
		},
		Database:     database,
		Connectors:   connectorVersions,
		Runtime:      runtimeInfo,
		Outcomes:     outcomes,
		RecentErrors: recentErrors,
		Redaction: RedactionInfo{
			Policy: "strict_allowlist",
			ExcludedByDesign: []string{
				"credentials", "tokens", "endpoints", "target_addresses", "target_names",
				"profile_names", "database_names", "commands", "action_payloads", "message_content",
				"raw_output", "raw_errors", "private_paths",
			},
			ErrorDetail: "category_only",
		},
	}, nil
}

func collectDatabaseInfo(ctx context.Context, database *sql.DB, supportedVersion int) (DatabaseInfo, error) {
	var migrationCount, schemaVersion int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&migrationCount, &schemaVersion); err != nil {
		return DatabaseInfo{}, fmt.Errorf("collect diagnostics migration state: %w", err)
	}
	var cipherVersion string
	if err := database.QueryRowContext(ctx, `PRAGMA cipher_version`).Scan(&cipherVersion); err != nil {
		return DatabaseInfo{}, fmt.Errorf("collect diagnostics cipher version: %w", err)
	}
	state := "current"
	switch {
	case schemaVersion < supportedVersion:
		state = "incomplete"
	case schemaVersion > supportedVersion:
		state = "newer_than_build"
	case migrationCount != supportedVersion:
		state = "inconsistent"
	}
	return DatabaseInfo{
		Encrypted:              true,
		SchemaVersion:          schemaVersion,
		SupportedSchemaVersion: supportedVersion,
		MigrationCount:         migrationCount,
		MigrationState:         state,
		SQLCipherVersion:       safeVersion(cipherVersion),
	}, nil
}

func collectRuntimeInfo(ctx context.Context, database *sql.DB, mcpEnabled bool, audit AuditHealth) (RuntimeInfo, OutcomeInfo, error) {
	counts := []struct {
		query string
		value *int64
	}{
		{`SELECT COUNT(*) FROM connector_action_requests WHERE status = 'running'`, new(int64)},
		{`SELECT COUNT(*) FROM connector_action_requests WHERE status = 'approval_pending'`, new(int64)},
		{`SELECT COUNT(*) FROM console_sessions WHERE status IN ('connecting', 'connected')`, new(int64)},
		{`SELECT COUNT(*) FROM file_transfers WHERE status IN ('pending', 'pending_approval', 'running', 'paused')`, new(int64)},
		{`SELECT COUNT(*) FROM history_entries WHERE status = 'outcome_unknown'`, new(int64)},
		{`SELECT COUNT(*) FROM history_entries WHERE status = 'outcome_unknown' AND julianday(updated_at) >= julianday('now', '-1 day')`, new(int64)},
	}
	for _, item := range counts {
		if err := database.QueryRowContext(ctx, item.query).Scan(item.value); err != nil {
			return RuntimeInfo{}, OutcomeInfo{}, fmt.Errorf("collect diagnostics runtime count: %w", err)
		}
	}
	mcpStatus := "stopped"
	if mcpEnabled {
		mcpStatus = "started"
	}
	return RuntimeInfo{
		Gateway: "running", Database: "unlocked", MCP: mcpStatus, Audit: audit,
		RunningActions: *counts[0].value, PendingApprovals: *counts[1].value,
		OpenConsoles: *counts[2].value, OpenTransfers: *counts[3].value,
	}, OutcomeInfo{UnknownTotal: *counts[4].value, UnknownLast24Hours: *counts[5].value}, nil
}

func collectRecentErrors(ctx context.Context, database *sql.DB) ([]RecentErrorSummary, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT connector_kind, activity_type, status, error, updated_at
		FROM history_entries
		WHERE status IN ('failed', 'error', 'blocked', 'stale', 'outcome_unknown')
		ORDER BY updated_at DESC, id DESC
		LIMIT ?`, recentErrorReadLimit)
	if err != nil {
		return nil, fmt.Errorf("collect diagnostics recent errors: %w", err)
	}
	defer rows.Close()

	type summaryKey struct{ connectorKind, activityType, status, category string }
	grouped := map[summaryKey]RecentErrorSummary{}
	for rows.Next() {
		var connectorKind, activityType, status, errorText, updatedAt string
		if err := rows.Scan(&connectorKind, &activityType, &status, &errorText, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan diagnostics recent error: %w", err)
		}
		key := summaryKey{
			connectorKind: safeIdentifier(connectorKind),
			activityType:  safeIdentifier(activityType),
			status:        safeIdentifier(status),
			category:      classifyError(status, errorText),
		}
		summary := grouped[key]
		summary.ConnectorKind = key.connectorKind
		summary.ActivityType = key.activityType
		summary.Status = key.status
		summary.Category = key.category
		summary.Count++
		if summary.LatestAt == "" {
			summary.LatestAt = safeTimestamp(updatedAt)
		}
		grouped[key] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate diagnostics recent errors: %w", err)
	}

	result := make([]RecentErrorSummary, 0, len(grouped))
	for _, summary := range grouped {
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LatestAt != result[j].LatestAt {
			return result[i].LatestAt > result[j].LatestAt
		}
		left := result[i].ConnectorKind + ":" + result[i].ActivityType + ":" + result[i].Status + ":" + result[i].Category
		right := result[j].ConnectorKind + ":" + result[j].ActivityType + ":" + result[j].Status + ":" + result[j].Category
		return left < right
	})
	if len(result) > recentErrorOutputLimit {
		result = result[:recentErrorOutputLimit]
	}
	return result, nil
}

func safeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return "unknown"
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune(" ._+-", character) {
			continue
		}
		return "unknown"
	}
	return value
}
