package connectors

import "context"

type TransferProgress func(transferred int64, total int64)

type TransferOptions struct {
	Progress      TransferProgress
	Wait          func(context.Context) error
	MaxBytes      int64
	RecordStaging func(context.Context, string) error
	ClearStaging  func(context.Context, string) error
}

type TransferResult struct {
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
