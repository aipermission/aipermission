package api

import (
	"time"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	mcpInitialExecTimeout       = 45 * time.Second
	mcpBackgroundCommandTimeout = 30 * time.Minute
)

type mcpAuthContext struct {
	TokenID int64
	Name    string
	runtime *databaseRuntime
}

type mcpServerItem struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Host          string   `json:"host,omitempty"`
	Port          int      `json:"port,omitempty"`
	Username      string   `json:"username,omitempty"`
	ExecutionRule string   `json:"execution_rule"`
	ExpiresAt     string   `json:"expires_at,omitempty"`
	Hints         []string `json:"hints"`
}

type mcpExecRequest struct {
	ServerID int64  `json:"server_id"`
	Command  string `json:"command"`
	Reason   string `json:"reason,omitempty"`
}

type mcpExecResponse struct {
	Status            string  `json:"status"`
	RequestID         int64   `json:"request_id,omitempty"`
	SessionID         int64   `json:"session_id,omitempty"`
	ServerID          int64   `json:"server_id"`
	ServerName        string  `json:"server_name,omitempty"`
	Command           string  `json:"command"`
	Stdout            string  `json:"stdout,omitempty"`
	Stderr            string  `json:"stderr,omitempty"`
	ExitCode          int     `json:"exit_code,omitempty"`
	Error             string  `json:"error,omitempty"`
	UserNote          *string `json:"user_note"`
	DurationMS        int64   `json:"duration_ms,omitempty"`
	RetryAfterSeconds int     `json:"retry_after_seconds,omitempty"`
	AssistantHint     string  `json:"assistant_hint,omitempty"`
}

type mcpConsoleResponse struct {
	Status     string  `json:"status"`
	SessionID  int64   `json:"session_id,omitempty"`
	ServerID   int64   `json:"server_id"`
	ServerName string  `json:"server_name,omitempty"`
	Transcript string  `json:"transcript,omitempty"`
	Error      string  `json:"error,omitempty"`
	UserNote   *string `json:"user_note,omitempty"`
}

type commandRequestRecord struct {
	ID                int64                `json:"id"`
	TokenID           *int64               `json:"token_id,omitempty"`
	TokenName         string               `json:"token_name,omitempty"`
	ServerID          int64                `json:"server_id"`
	ServerName        string               `json:"server_name"`
	Source            string               `json:"source"`
	Command           string               `json:"command"`
	Reason            string               `json:"reason"`
	Status            string               `json:"status"`
	TrackingReason    string               `json:"tracking_reason,omitempty"`
	OutputTruncated   bool                 `json:"output_truncated,omitempty"`
	Stdout            string               `json:"stdout,omitempty"`
	Stderr            string               `json:"stderr,omitempty"`
	ExitCode          *int                 `json:"exit_code,omitempty"`
	SessionID         *int64               `json:"session_id,omitempty"`
	UserNote          *string              `json:"user_note,omitempty"`
	Error             string               `json:"error,omitempty"`
	CreatedAt         string               `json:"created_at"`
	CompletedAt       *string              `json:"completed_at,omitempty"`
	RetryAfterSeconds int                  `json:"retry_after_seconds,omitempty"`
	AssistantHint     string               `json:"assistant_hint,omitempty"`
	Labels            []historyLabelRecord `json:"labels"`
}

type historyLabelRecord struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func mcpServerHints(executionRule string) []string {
	hints := []string{
		"Use a short reason when calling exec so the operator can approve or audit the command.",
		"For install/uninstall verification in the same shell, run 'hash -r 2>/dev/null || true' before checking command -v.",
		"On Debian/Ubuntu, dpkg -l can show removed packages as rc; use dpkg-query installed state 'ii' to verify packages are installed.",
		"Prefer non-interactive commands; set DEBIAN_FRONTEND=noninteractive for apt operations and use -y only when the action is intentional.",
		"For long or noisy output, use bounded commands such as tail -n, journalctl --no-pager -n, docker logs --tail, or redirect full output to a temp file.",
		"Before destructive actions, inspect first and make the destructive command explicit in the reason.",
		"Use absolute paths or 'cd /path && command' when directory context matters.",
		"Avoid printing secrets; inspect whether a file/key exists before catting environment files or credential paths.",
		"Read console output before sending another long-running command to the same server.",
	}
	if executionRule == tokens.RuleApprovalRequired {
		hints = append(hints, "This server requires approval; after exec returns approval_pending, poll get_request until it is completed, failed, or declined.")
	}
	return hints
}
