package gatewayconnectorapi

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type FileUploadReference struct {
	Principal    Principal
	TargetRef    string
	Ref          string
	UploadAction string
}

type FileUploadDispatch struct {
	FileUploadReference
	RuntimeID     int64
	Authorization TransferAuthorization
	Manifest      connectors.FileUploadManifest
}

type FileUploadStatus struct {
	Status           string `json:"status"`
	TransferID       int64  `json:"transfer_id"`
	TransferredBytes int64  `json:"transferred_bytes"`
	FailureKind      string `json:"failure_kind,omitempty"`
	ActionRequestID  int64  `json:"action_request_id,omitempty"`
}

type FileUploadGateway interface {
	ConnectorLaunchFileUpload(context.Context, FileUploadDispatch) (FileUploadStatus, error)
	ConnectorFileUploadStatus(context.Context, FileUploadReference) (FileUploadStatus, error)
}

func TransferAuthorizationFor(runtime connectors.RuntimeContext) TransferAuthorization {
	return TransferAuthorization{
		ConnectorKind: runtime.Target.ConnectorKind, TargetID: runtime.Target.ID,
		TargetRef: runtime.Target.Ref, TargetUpdatedAt: runtime.Target.UpdatedAt,
		ProfileID: runtime.Profile.ID, ProfileUpdatedAt: runtime.Profile.UpdatedAt,
		ProfileSecretRevision: runtime.Profile.SecretRevision,
	}
}
