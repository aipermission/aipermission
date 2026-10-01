package connectors

import "context"

// TransferProgress reports an absolute byte count for one attempt. Callbacks
// must finish before the transfer method returns.
type TransferProgress func(transferred int64, total int64)

type TransferOptions struct {
	Progress      TransferProgress
	Wait          func(context.Context) error
	MaxBytes      int64
	RecordStaging func(context.Context, string) error
	ClearStaging  func(context.Context, string) error
}

type TransferResult struct {
	// For downloads Bytes includes successfully written bytes even on failure.
	// The caller owns partial local staging and its cleanup after final evidence.
	Bytes          int64
	Size           int64
	ChecksumSHA256 string
	DurationMS     int64
}

type RemoteFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type RemotePathStatus struct {
	Exists bool   `json:"exists"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
}
