package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const MaximumFileUploadBytes = int64(512 << 20)

var ErrUploadContentChanged = errors.New("upload content no longer matches the approved size and checksum; destination was not published")
var ErrUploadStageExpired = errors.New("the staged upload is no longer available; submit a new upload for approval")
var ErrUploadStagingCapacity = errors.New("MCP upload staging capacity is exhausted; complete or cancel existing uploads before retrying")
var ErrUploadStageConflict = errors.New("upload idempotency key was already used or its result expired")
var ErrInvalidUploadManifest = errors.New("invalid upload manifest")

type FileContentIdentity struct {
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

type FileUploadManifest struct {
	FileName       string `json:"file_name"`
	RemotePath     string `json:"remote_path"`
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	Overwrite      bool   `json:"overwrite"`
}

type FileUploadStageRequest struct {
	RuntimeID  int64
	Ref        string
	TargetRef  string
	ActionName string
	Manifest   FileUploadManifest
}

type FileUploadStager func(context.Context, FileUploadStageRequest, io.Reader) error

type FileUploadApproval struct {
	TokenID    int64
	RequestID  int64
	TargetRef  string
	ActionName string
	Ref        string
}

type FileUploadApprovalCheck func(context.Context, FileUploadApproval) (bool, error)

func (m FileUploadManifest) ContentIdentity() FileContentIdentity {
	return FileContentIdentity{SizeBytes: m.SizeBytes, ChecksumSHA256: m.ChecksumSHA256}
}

func (m FileUploadManifest) ActionInput(ref string) map[string]any {
	return map[string]any{
		"upload_ref": ref, "file_name": m.FileName, "remote_path": m.RemotePath,
		"size_bytes": m.SizeBytes, "checksum_sha256": m.ChecksumSHA256, "overwrite": m.Overwrite,
	}
}

func FileUploadManifestFromInput(input map[string]any) FileUploadManifest {
	return FileUploadManifest{
		FileName: StringMapValue(input, "file_name"), RemotePath: StringMapValue(input, "remote_path"),
		SizeBytes: int64(IntMapValue(input, "size_bytes", -1)), ChecksumSHA256: StringMapValue(input, "checksum_sha256"),
		Overwrite: BoolMapValue(input, "overwrite", false),
	}
}

func (identity FileContentIdentity) Validate() error {
	if identity.SizeBytes < 0 || identity.SizeBytes > MaximumFileUploadBytes {
		return fmt.Errorf("upload size_bytes must be between 0 and %d", MaximumFileUploadBytes)
	}
	digest, err := hex.DecodeString(identity.ChecksumSHA256)
	if err != nil || len(digest) != sha256.Size || identity.ChecksumSHA256 != strings.ToLower(identity.ChecksumSHA256) {
		return errors.New("checksum_sha256 must be a lowercase 64-character SHA-256 digest")
	}
	return nil
}

func (identity FileContentIdentity) Verify(size int64, digest string) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	if size != identity.SizeBytes || digest != identity.ChecksumSHA256 {
		return ErrUploadContentChanged
	}
	return nil
}

// FileUploadInputSchema describes metadata only. The generic MCP upload bridge
// injects upload_ref after staging; file bytes never enter the action payload.
func FileUploadInputSchema() Schema {
	return Schema{Fields: []Field{
		{Name: "upload_ref", Label: "Staged upload reference", Type: FieldString, Required: true, PreserveWhitespace: true, Description: "Gateway-owned reference supplied by upload_connector_file; do not invent a staging path."},
		{Name: "file_name", Label: "Filename", Type: FieldString, Required: true, PreserveWhitespace: true, Description: "Exact source basename bound to the staged file."},
		{Name: "remote_path", Label: "Destination", Type: FieldString, Required: true, PreserveWhitespace: true, Description: "Exact destination file, not a directory."},
		{Name: "size_bytes", Label: "File size", Type: FieldInteger, Required: true, Description: "Verified staged size in bytes, including zero for an empty file."},
		{Name: "checksum_sha256", Label: "SHA-256", Type: FieldString, Required: true, PreserveWhitespace: true, Description: "Verified lowercase SHA-256 digest of the staged file."},
		{Name: "overwrite", Label: "Overwrite", Type: FieldBoolean, Default: false, Description: "Leave false unless replacing the destination is explicitly authorized."},
	}}
}
