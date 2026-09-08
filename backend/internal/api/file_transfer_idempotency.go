package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

const fileTransferUIIdempotencyScope = "ui:file-transfer-start"

type fileTransferUploadIdentityItem struct {
	RemotePath string `json:"remote_path"`
	FileName   string `json:"file_name"`
	SizeBytes  int64  `json:"size_bytes"`
	Checksum   string `json:"checksum_sha256"`
}

func fileTransferStartClaim(key, resourceKind string, identity any) (filetransfer.IdempotencyClaim, error) {
	payload, err := json.Marshal(identity)
	if err != nil {
		return filetransfer.IdempotencyClaim{}, err
	}
	digest := sha256.Sum256(payload)
	return filetransfer.IdempotencyClaim{
		Scope:        fileTransferUIIdempotencyScope,
		Key:          strings.TrimSpace(key),
		IdentityHash: "h1:" + hex.EncodeToString(digest[:]),
		ResourceKind: resourceKind,
	}, nil
}

func requireFileTransferIdempotencyKey(w http.ResponseWriter, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		writeError(w, http.StatusBadRequest, "idempotency_key is required")
		return false
	}
	if len(key) > filetransfer.MaxIdempotencyKeyBytes {
		writeError(w, http.StatusBadRequest, "idempotency_key is too long")
		return false
	}
	return true
}

func writeFileTransferIdempotencyError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, filetransfer.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
		return true
	case errors.Is(err, filetransfer.ErrIdempotencyResultExpired):
		writeError(w, http.StatusGone, "the original file transfer result expired; retry with a new idempotency key")
		return true
	default:
		return false
	}
}
