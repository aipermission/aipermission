package gatewayconnectorapi

import (
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	RuntimeCapabilityLiveConsole  = "live_console"
	RuntimeCapabilityFileTransfer = "file_transfer"
)

type RuntimeSurface struct {
	ID             int64
	ConnectorKind  string
	TargetID       int64
	ProfileID      int64
	CapabilityKind string
	Label          string
	Status         string
	CreatedAt      string
	UpdatedAt      string
}

type EnsureRuntimeSurfaceInput struct {
	ConnectorKind  string
	TargetID       int64
	ProfileID      int64
	CapabilityKind string
	Label          string
}

type Target struct {
	ID            int64
	ProjectID     int64
	ProjectName   string
	ProjectSlug   string
	ConnectorKind string
	Name          string
	Config        map[string]any
	Status        string
	CreatedAt     string
	UpdatedAt     string
}

type CredentialProfile struct {
	ID                  int64
	TargetID            int64
	ConnectorKind       string
	Kind                string
	Label               string
	Public              map[string]any
	EncryptedSecretJSON string
	RiskLabel           string
	SecretRevision      int64
	CreatedAt           string
	UpdatedAt           string
}

type ActionRequest struct {
	ID            int64
	ConnectorKind string
	ActionName    string
	Status        connectors.ResultStatus
}

type PrincipalKind string

const (
	PrincipalLocalOperator PrincipalKind = "local_operator"
	PrincipalMCPToken      PrincipalKind = "mcp_token"
)

var ErrInvalidPrincipal = errors.New("connector execution principal is required")

type Principal struct {
	Kind              PrincipalKind
	TokenID           int64
	WorkspaceID       string
	RuntimeInstanceID string
}

func (principal Principal) Validate() error {
	if err := (connectors.Principal{
		Kind: connectors.PrincipalKind(principal.Kind), TokenID: principal.TokenID,
		WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
	}).Validate(); err != nil {
		return ErrInvalidPrincipal
	}
	return nil
}

type ConsoleSessionHandle struct {
	ID         int64 `json:"id"`
	RuntimeID  int64 `json:"runtime_id"`
	Generation int64 `json:"generation"`
}

func (handle ConsoleSessionHandle) Valid() bool {
	return handle.ID > 0 && handle.RuntimeID > 0 && handle.Generation > 0
}

type ConsoleExecResult struct {
	SessionID  int64
	Generation int64
	Command    string
	Output     string
	ExitCode   int
	Running    bool
	DurationMS int64
}

type ConsoleRecord struct {
	ID                     int64   `json:"id"`
	RuntimeID              int64   `json:"runtime_id"`
	Generation             int64   `json:"generation"`
	TargetName             string  `json:"target_name"`
	Name                   string  `json:"name"`
	Status                 string  `json:"status"`
	Transcript             string  `json:"transcript"`
	Error                  string  `json:"error"`
	Cols                   int     `json:"cols"`
	Rows                   int     `json:"rows"`
	CreatedAt              string  `json:"created_at"`
	UpdatedAt              string  `json:"updated_at"`
	ClosedAt               *string `json:"closed_at"`
	EnvironmentContentHash string  `json:"environment_content_hash,omitempty"`
}

// SessionEnvironment is the connector boundary for an in-memory secret
// envelope. Implementations retain ownership of value buffers; callbacks must
// not retain value after returning.
type SessionEnvironment interface {
	Len() int
	ForEach(func(name string, value []byte, replaceExisting bool, itemID int64, valueVersion int64, sourceProjectID int64) error) error
}
