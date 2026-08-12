package diagnostics

const ReportFormatVersion = "1"

type Report struct {
	ReportFormatVersion string               `json:"report_format_version"`
	GeneratedAt         string               `json:"generated_at"`
	Application         ApplicationInfo      `json:"application"`
	Architecture        ArchitectureInfo     `json:"architecture"`
	Database            DatabaseInfo         `json:"database"`
	Connectors          []ConnectorInfo      `json:"connectors"`
	Runtime             RuntimeInfo          `json:"runtime"`
	Outcomes            OutcomeInfo          `json:"outcomes"`
	RecentErrors        []RecentErrorSummary `json:"recent_errors"`
	Redaction           RedactionInfo        `json:"redaction"`
}

type ApplicationInfo struct {
	Service string `json:"service"`
	Version string `json:"version"`
}

type ArchitectureInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
}

type DatabaseInfo struct {
	Encrypted              bool   `json:"encrypted"`
	SchemaVersion          int    `json:"schema_version"`
	SupportedSchemaVersion int    `json:"supported_schema_version"`
	MigrationCount         int    `json:"migration_count"`
	MigrationState         string `json:"migration_state"`
	SQLCipherVersion       string `json:"sqlcipher_version"`
}

type ConnectorInfo struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

type RuntimeInfo struct {
	Gateway          string      `json:"gateway"`
	Database         string      `json:"database"`
	MCP              string      `json:"mcp"`
	Audit            AuditHealth `json:"audit"`
	RunningActions   int64       `json:"running_actions"`
	PendingApprovals int64       `json:"pending_approvals"`
	OpenConsoles     int64       `json:"open_consoles"`
	OpenTransfers    int64       `json:"open_transfers"`
}

type AuditHealth struct {
	Status            string `json:"status"`
	FailureCount      uint64 `json:"failure_count"`
	PendingCount      int64  `json:"pending_count"`
	DeadLetterCount   int64  `json:"dead_letter_count"`
	RetriedEventCount int64  `json:"retried_event_count"`
}

type OutcomeInfo struct {
	UnknownTotal       int64 `json:"unknown_total"`
	UnknownLast24Hours int64 `json:"unknown_last_24_hours"`
}

type RecentErrorSummary struct {
	ConnectorKind string `json:"connector_kind"`
	ActivityType  string `json:"activity_type"`
	Status        string `json:"status"`
	Category      string `json:"category"`
	Count         int64  `json:"count"`
	LatestAt      string `json:"latest_at,omitempty"`
}

type RedactionInfo struct {
	Policy           string   `json:"policy"`
	ExcludedByDesign []string `json:"excluded_by_design"`
	ErrorDetail      string   `json:"error_detail"`
}
