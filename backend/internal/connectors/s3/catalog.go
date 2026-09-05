package s3connector

import (
	"context"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{
			Name:        "connection_mode",
			Label:       "Connection mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "direct",
			Description: "Connect directly from the local gateway, or tunnel to an S3-compatible endpoint through an SSH connector profile.",
			Options: []connectors.FieldOption{
				{Value: "direct", Label: "Direct"},
				{Value: "over_ssh", Label: "Over SSH"},
			},
		},
		{
			Name:        "scheme",
			Label:       "Scheme",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     defaultS3Scheme,
			Description: "Endpoint HTTP scheme.",
			Options: []connectors.FieldOption{
				{Value: "http", Label: "HTTP"},
				{Value: "https", Label: "HTTPS"},
			},
		},
		{
			Name:        "host",
			Label:       "Endpoint host",
			Type:        connectors.FieldString,
			Required:    true,
			Default:     defaultS3Host,
			Description: "S3-compatible endpoint host. For Over SSH this is resolved from the SSH server.",
		},
		{
			Name:        "port",
			Label:       "Endpoint port",
			Type:        connectors.FieldInteger,
			Required:    true,
			Default:     defaultS3Port,
			Description: "Endpoint TCP port.",
		},
		{
			Name:        "region",
			Label:       "Region",
			Type:        connectors.FieldString,
			Required:    true,
			Default:     defaultS3Region,
			Description: "AWS SigV4 region. Many S3-compatible services accept us-east-1.",
		},
		{
			Name:        "bucket",
			Label:       "Bucket",
			Type:        connectors.FieldString,
			Required:    true,
			Description: "Bucket name to browse and manage.",
		},
		{
			Name:        "path_style",
			Label:       "Path-style addressing",
			Type:        connectors.FieldBoolean,
			Default:     true,
			Description: "Use /bucket/key paths. Keep enabled for most S3-compatible providers such as MinIO.",
		},
		{
			Name:        "trust_conditional_requests",
			Label:       "Verified conditional requests",
			Type:        connectors.FieldBoolean,
			Default:     false,
			Description: "Enable condition-dependent mutations only after this provider's destination If-Match and If-None-Match behavior has been verified.",
		},
		{
			Name:        "transport_target_ref",
			Label:       "SSH transport target",
			Type:        connectors.FieldString,
			Description: "Connector target ref used when connection_mode is over_ssh.",
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "access_key",
			Label:       "Access key",
			Description: "S3 access key credentials stored through the encrypted vault layer.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "access_key_id",
					Label:       "Access key ID",
					Type:        connectors.FieldString,
					Required:    true,
					Description: "S3 access key ID.",
				},
				{
					Name:        "secret_access_key",
					Label:       "Secret access key",
					Type:        connectors.FieldSecret,
					Required:    true,
					Secret:      true,
					Description: "S3 secret access key.",
				},
				{
					Name:        "session_token",
					Label:       "Session token",
					Type:        connectors.FieldSecret,
					Secret:      true,
					Description: "Optional temporary session token.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "S3 target"
	if strings.TrimSpace(target.Name) != "" {
		title = "S3 target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Browse S3-compatible object storage and run bounded object actions through AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use bucket_info first when you need to verify bucket reachability or endpoint metadata.",
			"Use list_objects with prefix to browse folders. Folder entries return browse_input; call list_objects with that input to enter the folder.",
			"Use list_objects with cursor from next_cursor to fetch the next page. Do not send continuation_token; use the cursor field only.",
			"Use search for bounded key lookup across pages. When search is set, folder grouping is disabled and results are returned as matching objects.",
			"Use get_object_metadata to inspect one object without downloading content.",
			"Use download_object only for bounded object reads. It returns base64 content for the requested object up to max_bytes.",
			"Use upload_object with overwrite=false by default. If the object exists, ask the operator before retrying with overwrite=true.",
			"Use delete_object carefully; it is destructive and should normally require explicit approval.",
			"Use presign_download or presign_upload only when the operator explicitly needs a short-lived URL for one exact object key.",
			"Use list_object_versions before restoring or deleting one exact version. Restoring creates a new current version.",
			"Read the current bucket lifecycle before changing it. replace_bucket_lifecycle deliberately replaces the complete policy with one bounded rule.",
		},
		Warnings: []string{
			"S3 objects may contain secrets or customer data. Redaction is best-effort; avoid reading object content unless explicitly approved.",
			"download_object and upload_object are intentionally size-bounded in the connector action pipeline.",
			"Do not put access keys, secret keys, signed URLs, or reusable tokens into action input. Store credentials in the selected credential profile.",
			"S3 credential profiles decide what the object storage service itself allows.",
			"Presigned URLs are temporary bearer credentials. Do not place them in reasons, inputs, logs, or messages beyond the intended recipient.",
			"delete_object_version permanently removes one stored version or delete marker and is destructive.",
			"Lifecycle replacement and deletion can affect object retention. Treat both as destructive operations.",
			"rename_object is intentionally unavailable because S3-compatible APIs do not provide an atomic cross-key move. Keep the source intact and treat any later source deletion as a separate destructive decision.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	actions := objectActions()
	actions = append(actions, signedURLAndVersionActions()...)
	actions = append(actions, lifecycleActions()...)
	return actions, nil
}

func objectActions() []connectors.ActionDefinition {
	return []connectors.ActionDefinition{
		{
			Name:        ActionBucketInfo,
			Label:       "Bucket info",
			Description: "Check bucket access and read bounded bucket metadata.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionListObjects,
			Label:       "List objects",
			Description: "List objects in the bucket with optional prefix/search filters.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "prefix", Label: "Prefix", Type: connectors.FieldString, PreserveWhitespace: true, Description: "Optional object key prefix. Use a folder prefix ending in / to browse inside that folder."},
				{Name: "search", Label: "Search", Type: connectors.FieldString, Description: "Optional case-insensitive key search applied across bounded list pages. Folder grouping is disabled while searching."},
				{Name: "cursor", Label: "Cursor", Type: connectors.FieldString, Description: "Optional pagination cursor returned as next_cursor by a previous list_objects response."},
				{Name: "limit", Label: "Limit", Type: connectors.FieldInteger, Default: defaultS3ListLimit, Description: "Maximum objects to return, capped by the connector."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxS3ListLimit},
		},
		{
			Name:        ActionGetObjectMetadata,
			Label:       "Read object metadata",
			Description: "Read headers and metadata for one object without downloading content.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact object key returned by list_objects."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 32 << 10},
		},
		{
			Name:        ActionDownloadObject,
			Label:       "Download object",
			Description: "Download one bounded object and return base64 content.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact object key returned by list_objects."},
				{Name: "max_bytes", Label: "Max bytes", Type: connectors.FieldInteger, Default: defaultDownloadMax, Description: "Maximum bytes to read, capped by the connector."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: maxDownloadBytes},
		},
		{
			Name:        ActionUploadObject,
			Label:       "Upload object",
			Description: "Upload one bounded object from text or base64 content.",
			Category:    "write",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Destination object key."},
				{Name: "content_text", Label: "Text content", Type: connectors.FieldMultiline, Description: "Text payload for small text objects. Use this or content_base64, not both."},
				{Name: "content_base64", Label: "Base64 content", Type: connectors.FieldMultiline, Description: "Base64 payload for binary objects. Use this or content_text, not both."},
				{Name: "content_type", Label: "Content type", Type: connectors.FieldString, Default: "application/octet-stream", Description: "Object content type to send with the upload."},
				{Name: "overwrite", Label: "Overwrite existing object", Type: connectors.FieldBoolean, Default: false, Description: "Leave false unless the operator explicitly approved replacing an existing object."},
				{Name: "expected_etag", Label: "Expected ETag", Type: connectors.FieldString, Description: "Optional current ETag. When set, overwrite succeeds only if the object still has this ETag."},
			}},
			SensitiveInputFields: []string{"content_text", "content_base64"},
			OutputHint:           connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionDeleteObject,
			Label:       "Delete object",
			Description: "Delete one object from the bucket.",
			Category:    "destructive",
			Risk:        connectors.RiskDestructive,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact object key to delete."},
				{Name: "expected_etag", Label: "Expected ETag", Type: connectors.FieldString, Description: "Optional current ETag. When set, deletion succeeds only if the object still has this ETag."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
	}
}

func signedURLAndVersionActions() []connectors.ActionDefinition {
	return []connectors.ActionDefinition{
		{
			Name:        ActionPresignDownload,
			Label:       "Create download URL",
			Description: "Create a short-lived signed GET URL for one existing object.",
			Category:    "sharing",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact existing object key."},
				{Name: "expires_seconds", Label: "Expires in seconds", Type: connectors.FieldInteger, Default: defaultPresignedExpirySeconds, Description: "URL lifetime from 60 to 3600 seconds."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 8000, TemporaryCapabilityFields: []string{"url"}},
		},
		{
			Name:        ActionPresignUpload,
			Label:       "Create upload URL",
			Description: "Create a short-lived signed PUT URL for one exact object key.",
			Category:    "sharing",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact destination object key."},
				{Name: "expires_seconds", Label: "Expires in seconds", Type: connectors.FieldInteger, Default: defaultPresignedExpirySeconds, Description: "URL lifetime from 60 to 3600 seconds."},
				{Name: "overwrite", Label: "Allow overwrite", Type: connectors.FieldBoolean, Default: false, Description: "Leave false unless replacing an existing object is intentional."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 8000, TemporaryCapabilityFields: []string{"url"}},
		},
		{
			Name:        ActionListVersions,
			Label:       "List object versions",
			Description: "List bounded stored versions and delete markers for one exact object key.",
			Category:    "versioning",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact object key."},
				{Name: "cursor", Label: "Cursor", Type: connectors.FieldString, Description: "Optional cursor returned by the previous page."},
				{Name: "limit", Label: "Limit", Type: connectors.FieldInteger, Default: defaultVersionListLimit, Description: "Maximum version records requested from S3."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 16000},
		},
		{
			Name:        ActionRestoreVersion,
			Label:       "Restore object version",
			Description: "Copy one stored object version into a new current version.",
			Category:    "versioning",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact object key."},
				{Name: "version_id", Label: "Version ID", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact stored version ID returned by list_object_versions."},
				{Name: "expected_current_etag", Label: "Expected current ETag", Type: connectors.FieldString, Description: "Current destination ETag read immediately before approval. Mutually exclusive with expected_current_absent."},
				{Name: "expected_current_absent", Label: "Expect current object to be absent", Type: connectors.FieldBoolean, Default: false, Description: "Use only after a metadata read confirms the destination key is absent. Mutually exclusive with expected_current_etag."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionDeleteVersion,
			Label:       "Delete object version",
			Description: "Permanently delete one exact object version or delete marker.",
			Category:    "destructive",
			Risk:        connectors.RiskDestructive,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact object key."},
				{Name: "version_id", Label: "Version ID", Type: connectors.FieldString, PreserveWhitespace: true, Required: true, Description: "Exact version ID returned by list_object_versions."},
			}},
			RetryPolicy: connectors.RetryPolicy{Class: connectors.RetryIdempotent},
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
	}
}

func lifecycleActions() []connectors.ActionDefinition {
	return []connectors.ActionDefinition{
		{
			Name:        ActionGetLifecycle,
			Label:       "Read bucket lifecycle",
			Description: "Read the current bucket lifecycle policy and summarize its rules.",
			Category:    "lifecycle",
			Risk:        connectors.RiskRead,
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: maxLifecycleResponse},
		},
		{
			Name:        ActionReplaceLifecycle,
			Label:       "Replace bucket lifecycle",
			Description: "Replace the complete bucket lifecycle policy with one bounded expiration rule.",
			Category:    "destructive",
			Risk:        connectors.RiskDestructive,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "rule_id", Label: "Rule ID", Type: connectors.FieldString, Required: true, Default: defaultLifecycleRuleID, Description: "Stable identifier for the replacement rule."},
				{Name: "prefix", Label: "Object prefix", Type: connectors.FieldString, PreserveWhitespace: true, Description: "Optional key prefix. Empty applies to the whole bucket."},
				{Name: "expire_current_after_days", Label: "Expire current after days", Type: connectors.FieldInteger, Default: 0, Description: "0 disables current-version expiration."},
				{Name: "expire_noncurrent_after_days", Label: "Expire noncurrent after days", Type: connectors.FieldInteger, Default: 0, Description: "0 disables noncurrent-version expiration."},
				{Name: "abort_incomplete_multipart_days", Label: "Abort multipart after days", Type: connectors.FieldInteger, Default: 7, Description: "0 disables incomplete multipart cleanup."},
				{Name: "enabled", Label: "Enabled", Type: connectors.FieldBoolean, Default: true, Description: "Store the replacement rule as enabled or disabled."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionDeleteLifecycle,
			Label:       "Delete bucket lifecycle",
			Description: "Delete the complete lifecycle policy from this bucket.",
			Category:    "destructive",
			Risk:        connectors.RiskDestructive,
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
	}
}
