package filetransfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	MaxMCPUploadBytes       = connectors.MaximumFileUploadBytes
	MaxMCPUploadStagedBytes = int64(1 << 30)
	MaxMCPUploadStagedFiles = 16
	MCPUploadTTL            = 15 * time.Minute
	mcpUploadScopePrefix    = "mcp-upload:"
	mcpUploadTimeLayout     = "2006-01-02T15:04:05.000000000Z"
)

var ErrMCPUploadCapacity = errors.New("MCP staged transfer quota reached")

func MCPUploadReady(item Record, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339Nano, item.TempExpiresAt)
	return item.Status == StatusPending && item.TempPath != "" && err == nil && now.Before(expires)
}

// UploadManifest is immutable approval metadata, never the file contents or a
// local source path. Remote path normalization remains connector-owned.
type UploadManifest connectors.FileUploadManifest

type UploadIdentity struct {
	TargetRef  string         `json:"target_ref"`
	ActionName string         `json:"action_name"`
	Manifest   UploadManifest `json:"manifest"`
}

type MCPUploadStage struct {
	TokenID        int64
	IdempotencyKey string
	Identity       UploadIdentity
	RuntimeID      int64
	TempPath       string
}

func (manifest UploadManifest) Validate() error {
	if err := ValidateFileName(manifest.FileName); err != nil {
		return err
	}
	if err := validatePathLike("remote_path", manifest.RemotePath, true); err != nil {
		return err
	}
	return (connectors.FileContentIdentity{SizeBytes: manifest.SizeBytes, ChecksumSHA256: manifest.ChecksumSHA256}).Validate()
}

func MCPUploadClaim(tokenID int64, key string, identity UploadIdentity) (IdempotencyClaim, error) {
	if tokenID < 1 {
		return IdempotencyClaim{}, errors.New("MCP upload token identity is required")
	}
	if key == "" || strings.TrimSpace(key) != key || strings.ContainsFunc(key, unicode.IsControl) {
		return IdempotencyClaim{}, errors.New("invalid upload idempotency_key")
	}
	kind, targetID, profileID, valid := connectors.ParseTargetRef(identity.TargetRef)
	if !valid || identity.TargetRef != connectors.FormatTargetRef(kind, targetID, profileID) || !connectors.ValidIdentifier(identity.ActionName) {
		return IdempotencyClaim{}, errors.New("MCP upload target and action identity are required")
	}
	if err := identity.Manifest.Validate(); err != nil {
		return IdempotencyClaim{}, err
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return IdempotencyClaim{}, err
	}
	digest := sha256.Sum256(encoded)
	return normalizeIdempotencyClaim(IdempotencyClaim{
		Scope: mcpUploadScope(tokenID), Key: key, IdentityHash: hex.EncodeToString(digest[:]),
		ResourceKind: IdempotencyResourceTransfer,
	}, IdempotencyResourceTransfer)
}

func mcpUploadScope(tokenID int64) string {
	return mcpUploadScopePrefix + strconv.FormatInt(tokenID, 10)
}
