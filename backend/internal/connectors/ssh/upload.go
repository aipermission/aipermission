package sshconnector

import (
	"errors"
	"path"
	"strings"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	ActionUploadFile          = "upload_file"
	ActionGetFileUploadStatus = "get_file_upload_status"
)

func fileUploadActions() []connectors.ActionDefinition {
	return []connectors.ActionDefinition{
		{
			Name: ActionUploadFile, Label: "Upload file", Category: "files", Risk: connectors.RiskWrite,
			Description: "Queue a staged file for SFTP upload. Use upload_connector_file; do not send bytes in the action input.",
			InputSchema: connectors.FileUploadInputSchema(), OutputHint: connectors.OutputHint{Format: "json"},
			MaxUploadBytes: connectors.MaximumFileUploadBytes,
		},
		{
			Name: ActionGetFileUploadStatus, Label: "Read upload status", Category: "files", Risk: connectors.RiskRead,
			Description: "Read the status of this token's upload on this SSH profile.",
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "upload_ref", Label: "Upload reference", Type: connectors.FieldString, Required: true},
			}},
			OutputHint: connectors.OutputHint{Format: "json"},
		},
	}
}

func prepareFileUpload(req connectors.ActionRequest, base connectors.PreparedAction) (connectors.PreparedAction, error) {
	ref := stringInput(req.Input, "upload_ref")
	if ref == "" || len(ref) > 128 || strings.TrimSpace(ref) != ref || strings.ContainsFunc(ref, unicode.IsControl) {
		return connectors.PreparedAction{}, errors.New("invalid upload_ref")
	}
	if req.ActionName == ActionGetFileUploadStatus {
		base.Title, base.Risk = "Read SSH upload status", connectors.RiskRead
		base.Summary = targetSummary(req.Target, "Read a file upload status")
		base.Payload = map[string]any{"upload_ref": ref}
		base.Preview = base.Payload
		base.ContextMaterial["upload_ref"] = ref
		return base, nil
	}
	manifest := connectors.FileUploadManifestFromInput(req.Input)
	destination := manifest.RemotePath
	if !path.IsAbs(destination) || path.Clean(destination) != destination || destination == "/" || strings.ContainsFunc(destination, unicode.IsControl) {
		return connectors.PreparedAction{}, errors.New("remote_path must be an absolute, canonical file path")
	}
	if err := manifest.ContentIdentity().Validate(); err != nil {
		return connectors.PreparedAction{}, err
	}
	if strings.TrimSpace(req.Reason) == "" {
		return connectors.PreparedAction{}, errors.New("an upload reason is required")
	}
	base.Title, base.Risk = "Upload SSH file", connectors.RiskWrite
	base.Summary = targetSummary(req.Target, "Upload a file over SFTP")
	base.Payload = manifest.ActionInput(ref)
	base.Preview = base.Payload
	base.ContextMaterial["upload"] = base.Payload
	return base, nil
}
