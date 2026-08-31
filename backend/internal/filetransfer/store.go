package filetransfer

import (
	"database/sql"
	"errors"
)

const (
	DirectionUpload   = "upload"
	DirectionDownload = "download"

	SourceUI  = "ui"
	SourceMCP = "mcp"

	StatusPending         = "pending"
	StatusPendingApproval = "pending_approval"
	StatusRunning         = "running"
	StatusPaused          = "paused"
	StatusCompleted       = "completed"
	StatusFailed          = "failed"
	StatusCanceled        = "canceled"

	FailureKindUnknown          = "unknown"
	FailureKindTimeout          = "timeout"
	FailureKindValidation       = "validation"
	FailureKindLocalPersistence = "local_persistence"
	FailureKindOutcomeUnknown   = "outcome_unknown"
	FailureKindInterrupted      = "interrupted"
)

const maxPathRunes = 4096

var ErrNotFound = errors.New("file transfer not found")
var ErrInvalidState = errors.New("file transfer invalid state")
var ErrInvalidArgument = errors.New("file transfer invalid argument")

type Record struct {
	ID               int64  `json:"id"`
	BatchID          int64  `json:"batch_id"`
	QueueIndex       int    `json:"queue_index"`
	RuntimeID        int64  `json:"runtime_id"`
	TargetName       string `json:"target_name"`
	Direction        string `json:"direction"`
	Source           string `json:"source"`
	Status           string `json:"status"`
	LocalPath        string `json:"local_path"`
	RemotePath       string `json:"remote_path"`
	FileName         string `json:"file_name"`
	SizeBytes        int64  `json:"size_bytes"`
	TransferredBytes int64  `json:"transferred_bytes"`
	BytesPerSecond   int64  `json:"bytes_per_second"`
	ETASeconds       int64  `json:"eta_seconds"`
	ChecksumSHA256   string `json:"checksum_sha256"`
	Error            string `json:"error"`
	FailureKind      string `json:"failure_kind,omitempty"`
	CreatedAt        string `json:"created_at"`
	StartedAt        string `json:"started_at,omitempty"`
	CompletedAt      string `json:"completed_at,omitempty"`
	UpdatedAt        string `json:"updated_at"`

	TempPath string `json:"-"`
}

type CreateRequest struct {
	BatchID          int64
	QueueIndex       int
	RuntimeID        int64
	Direction        string
	Source           string
	LocalPath        string
	RemotePath       string
	FileName         string
	SizeBytes        int64
	TransferredBytes int64
	TempPath         string
}

type BatchRecord struct {
	ID               int64    `json:"id"`
	RuntimeID        int64    `json:"runtime_id"`
	TargetName       string   `json:"target_name"`
	Direction        string   `json:"direction"`
	Source           string   `json:"source"`
	Status           string   `json:"status"`
	ArchiveName      string   `json:"archive_name"`
	ApprovalNote     string   `json:"approval_note"`
	Overwrite        bool     `json:"overwrite"`
	TotalItems       int      `json:"total_items"`
	CompletedItems   int      `json:"completed_items"`
	FailedItems      int      `json:"failed_items"`
	CanceledItems    int      `json:"canceled_items"`
	SizeBytes        int64    `json:"size_bytes"`
	TransferredBytes int64    `json:"transferred_bytes"`
	BytesPerSecond   int64    `json:"bytes_per_second"`
	ETASeconds       int64    `json:"eta_seconds"`
	Error            string   `json:"error"`
	FailureKind      string   `json:"failure_kind,omitempty"`
	CreatedAt        string   `json:"created_at"`
	StartedAt        string   `json:"started_at,omitempty"`
	CompletedAt      string   `json:"completed_at,omitempty"`
	UpdatedAt        string   `json:"updated_at"`
	Items            []Record `json:"items,omitempty"`

	ArchivePath string `json:"-"`
}

type CreateBatchRequest struct {
	RuntimeID    int64
	Direction    string
	Source       string
	Status       string
	ApprovalNote string
	Overwrite    bool
	ArchiveName  string
	Items        []CreateRequest
}

type BatchApprovalRequest struct {
	ApprovedItemIDs []int64
	Note            string
}

type ListFilter struct {
	Direction string
	Status    string
	RuntimeID int64
	TargetIDs []int64
	Query     string
	Limit     int
	Offset    int
}

type BatchListFilter struct {
	Direction string
	Status    string
	RuntimeID int64
	TargetIDs []int64
	Query     string
	Limit     int
	Offset    int
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}
