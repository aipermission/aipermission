package filetransfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	MaxMCPUploadBytes       = int64(512 << 20)
	MaxMCPUploadStagedBytes = int64(1 << 30)
	MaxMCPUploadStagedFiles = 16
	MCPUploadTTL            = 15 * time.Minute
	mcpUploadScopePrefix    = "mcp-upload:"
	mcpUploadTimeLayout     = "2006-01-02T15:04:05.000000000Z"
)

var ErrMCPUploadCapacity = errors.New("MCP upload staging capacity is exhausted; complete or cancel existing uploads before retrying")

// UploadManifest is immutable approval metadata, never the file contents or a
// local source path. Remote path normalization remains connector-owned.
type UploadManifest struct {
	FileName       string `json:"file_name"`
	RemotePath     string `json:"remote_path"`
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	Overwrite      bool   `json:"overwrite"`
}

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
	if manifest.SizeBytes < 0 || manifest.SizeBytes > MaxMCPUploadBytes {
		return fmt.Errorf("upload size_bytes must be between 0 and %d", MaxMCPUploadBytes)
	}
	decoded, err := hex.DecodeString(manifest.ChecksumSHA256)
	if err != nil || len(decoded) != sha256.Size || manifest.ChecksumSHA256 != strings.ToLower(manifest.ChecksumSHA256) {
		return errors.New("checksum_sha256 must be a lowercase 64-character SHA-256 digest")
	}
	return nil
}

func MCPUploadClaim(tokenID int64, key string, identity UploadIdentity) (IdempotencyClaim, error) {
	if tokenID < 1 {
		return IdempotencyClaim{}, errors.New("MCP upload token identity is required")
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
