package api

import "github.com/aipermission/aipermission/backend/internal/connectorapi"

type startDownloadRequest struct {
	RuntimeID      int64  `json:"runtime_id"`
	RemotePath     string `json:"remote_path"`
	IdempotencyKey string `json:"idempotency_key"`
}

type startDownloadBatchRequest struct {
	RuntimeID      int64    `json:"runtime_id"`
	RemotePaths    []string `json:"remote_paths"`
	ArchiveName    string   `json:"archive_name"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type browseRemoteFilesRequest struct {
	RuntimeID int64  `json:"runtime_id"`
	Path      string `json:"path"`
	Cursor    string `json:"cursor,omitempty"`
}

type browseRemoteFilesResponse struct {
	RuntimeID int64                          `json:"runtime_id"`
	Path      string                         `json:"path"`
	Parent    string                         `json:"parent"`
	Entries   []connectorapi.RemoteFileEntry `json:"entries"`
	Next      string                         `json:"next_cursor,omitempty"`
	HasMore   bool                           `json:"has_more"`
}

type expandRemoteFilesRequest struct {
	RuntimeID int64  `json:"runtime_id"`
	Path      string `json:"path"`
}
