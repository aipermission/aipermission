// Package s3connector defines the S3-compatible storage connector contract.
package s3connector

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "s3"
	Label   = "S3"
	Version = "0.3"

	ActionBucketInfo        = "bucket_info"
	ActionListObjects       = "list_objects"
	ActionGetObjectMetadata = "get_object_metadata"
	ActionDownloadObject    = "download_object"
	ActionUploadObject      = "upload_object"
	ActionRenameObject      = "rename_object"
	ActionDeleteObject      = "delete_object"
	ActionPresignDownload   = "presign_download"
	ActionPresignUpload     = "presign_upload"
	ActionListVersions      = "list_object_versions"
	ActionRestoreVersion    = "restore_object_version"
	ActionDeleteVersion     = "delete_object_version"
	ActionGetLifecycle      = "get_bucket_lifecycle"
	ActionReplaceLifecycle  = "replace_bucket_lifecycle"
	ActionDeleteLifecycle   = "delete_bucket_lifecycle"

	defaultS3Scheme    = "https"
	defaultS3Host      = "s3.amazonaws.com"
	defaultS3Port      = 443
	defaultS3Region    = "us-east-1"
	defaultS3ListLimit = 100
	maxS3ListLimit     = 1000
	maxS3SearchPages   = 20
	defaultDownloadMax = 5 << 20
	maxDownloadBytes   = 25 << 20
	maxUploadBytes     = 16 << 20
	maxS3ResponseBytes = 2 << 20
	maxS3ReasonBytes   = 2000
	s3HTTPTimeout      = 30 * time.Second
	emptySHA256Hex     = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

var (
	ErrUnsupportedAction = errors.New("unsupported s3 connector action")
	ErrMissingTransport  = errors.New("s3 connector network transport is unavailable")
	ErrMissingSecret     = errors.New("s3 connector credential is missing required secret")
	ErrInvalidConfig     = errors.New("s3 connector target config is invalid")
)

// Connector describes S3-compatible object storage as a connector-shaped
// target with bounded object browsing and explicit write/destructive actions.
type Connector struct{}

func New() Connector {
	return Connector{}
}

func (Connector) Kind() string {
	return Kind
}

func (Connector) Label() string {
	return Label
}

func (Connector) Version() string {
	return Version
}

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
				{Name: "prefix", Label: "Prefix", Type: connectors.FieldString, Description: "Optional object key prefix. Use a folder prefix ending in / to browse inside that folder."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact object key returned by list_objects."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact object key returned by list_objects."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Destination object key."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact object key to delete."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact existing object key."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact destination object key."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact object key."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact object key."},
				{Name: "version_id", Label: "Version ID", Type: connectors.FieldString, Required: true, Description: "Exact stored version ID returned by list_object_versions."},
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
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true, Description: "Exact object key."},
				{Name: "version_id", Label: "Version ID", Type: connectors.FieldString, Required: true, Description: "Exact version ID returned by list_object_versions."},
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
				{Name: "prefix", Label: "Object prefix", Type: connectors.FieldString, Description: "Optional key prefix. Empty applies to the whole bucket."},
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

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	input := copyMap(req.Input)
	risk := connectors.RiskRead
	title := ""
	summary := ""
	switch req.ActionName {
	case ActionBucketInfo:
		title = "Read S3 bucket info"
		summary = fmt.Sprintf("Check bucket %q access.", s3Bucket(req.Target))
	case ActionListObjects:
		prefix := strings.TrimSpace(stringValue(input, "prefix"))
		search := strings.TrimSpace(stringValue(input, "search"))
		cursor := strings.TrimSpace(stringValue(input, "cursor"))
		limit := clampedInt(input, "limit", defaultS3ListLimit, 1, maxS3ListLimit)
		input["prefix"] = prefix
		input["search"] = search
		input["cursor"] = cursor
		input["limit"] = limit
		title = "List S3 objects"
		summary = fmt.Sprintf("List up to %d object(s) in bucket %q.", limit, s3Bucket(req.Target))
	case ActionGetObjectMetadata:
		key := normalizeObjectKey(input, "key")
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		input["key"] = key
		title = "Read S3 object metadata"
		summary = key
	case ActionDownloadObject:
		key := normalizeObjectKey(input, "key")
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		maxBytes := clampedInt(input, "max_bytes", defaultDownloadMax, 1, maxDownloadBytes)
		input["key"] = key
		input["max_bytes"] = maxBytes
		title = "Download S3 object"
		summary = fmt.Sprintf("%s (max %d bytes)", key, maxBytes)
	case ActionUploadObject:
		risk = connectors.RiskWrite
		title = "Upload S3 object"
		var err error
		summary, err = prepareUploadInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
	case ActionRenameObject:
		return connectors.PreparedAction{}, errors.New("S3 rename_object is disabled: S3-compatible APIs do not provide an atomic cross-key move; upload or copy the destination while keeping the source intact, then let the operator decide whether to issue a separate destructive delete")
	case ActionDeleteObject:
		risk = connectors.RiskDestructive
		key := normalizeObjectKey(input, "key")
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		input["key"] = key
		expectedETag, err := normalizeOptionalETag(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["expected_etag"] = expectedETag
		title = "Delete S3 object"
		summary = key
	case ActionPresignDownload, ActionPresignUpload:
		key := normalizeObjectKey(input, "key")
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		expiresSeconds := clampedInt(input, "expires_seconds", defaultPresignedExpirySeconds, minPresignedExpirySeconds, maxPresignedExpirySeconds)
		input["key"] = key
		input["expires_seconds"] = expiresSeconds
		if req.ActionName == ActionPresignUpload {
			risk = connectors.RiskWrite
			input["overwrite"] = boolValue(input, "overwrite")
			title = "Create S3 upload URL"
		} else {
			delete(input, "overwrite")
			title = "Create S3 download URL"
		}
		summary = fmt.Sprintf("%s (%d seconds)", key, expiresSeconds)
	case ActionListVersions:
		key := normalizeObjectKey(input, "key")
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		cursor := strings.TrimSpace(stringValue(input, "cursor"))
		if _, err := decodeVersionCursor(cursor); err != nil {
			return connectors.PreparedAction{}, err
		}
		input["key"] = key
		input["cursor"] = cursor
		input["limit"] = clampedInt(input, "limit", defaultVersionListLimit, 1, maxVersionListLimit)
		title = "List S3 object versions"
		summary = key
	case ActionRestoreVersion, ActionDeleteVersion:
		key := normalizeObjectKey(input, "key")
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		versionID, err := normalizeVersionID(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["key"] = key
		input["version_id"] = versionID
		if req.ActionName == ActionDeleteVersion {
			delete(input, "expected_etag")
			risk = connectors.RiskDestructive
			title = "Delete S3 object version"
		} else {
			expectedCurrentETag, err := normalizeOptionalETagField(input, "expected_current_etag")
			if err != nil {
				return connectors.PreparedAction{}, err
			}
			expectedCurrentAbsent := boolValue(input, "expected_current_absent")
			if (expectedCurrentETag == "") == !expectedCurrentAbsent {
				return connectors.PreparedAction{}, fmt.Errorf("provide exactly one of expected_current_etag or expected_current_absent")
			}
			input["expected_current_etag"] = expectedCurrentETag
			input["expected_current_absent"] = expectedCurrentAbsent
			risk = connectors.RiskWrite
			title = "Restore S3 object version"
		}
		summary = fmt.Sprintf("%s @ %s", key, versionID)
	case ActionGetLifecycle:
		title = "Read S3 bucket lifecycle"
		summary = s3Bucket(req.Target)
	case ActionReplaceLifecycle:
		risk = connectors.RiskDestructive
		if _, ok := input["rule_id"]; !ok {
			input["rule_id"] = defaultLifecycleRuleID
		}
		if _, ok := input["enabled"]; !ok {
			input["enabled"] = true
		}
		input["rule_id"] = strings.TrimSpace(stringValue(input, "rule_id"))
		input["prefix"] = normalizeObjectPrefix(stringValue(input, "prefix"))
		for _, field := range []string{"expire_current_after_days", "expire_noncurrent_after_days", "abort_incomplete_multipart_days"} {
			input[field] = intValue(input, field)
		}
		input["enabled"] = boolValue(input, "enabled")
		if err := validateLifecycleInput(input); err != nil {
			return connectors.PreparedAction{}, err
		}
		title = "Replace S3 bucket lifecycle"
		summary = fmt.Sprintf("Replace all rules with %q.", input["rule_id"])
	case ActionDeleteLifecycle:
		risk = connectors.RiskDestructive
		title = "Delete S3 bucket lifecycle"
		summary = fmt.Sprintf("Delete all lifecycle rules from %q.", s3Bucket(req.Target))
	default:
		return connectors.PreparedAction{}, ErrUnsupportedAction
	}
	return finalizePreparedAction(req, input, risk, title, summary)
}

func prepareUploadInput(input map[string]any) (string, error) {
	key := normalizeObjectKey(input, "key")
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	contentText := stringValue(input, "content_text")
	contentBase64 := strings.TrimSpace(stringValue(input, "content_base64"))
	if contentText == "" && contentBase64 == "" {
		return "", fmt.Errorf("content_text or content_base64 is required")
	}
	if contentText != "" && contentBase64 != "" {
		return "", fmt.Errorf("provide content_text or content_base64, not both")
	}
	contentBytes, err := uploadBytes(contentText, contentBase64)
	if err != nil {
		return "", err
	}
	if len(contentBytes) > maxUploadBytes {
		return "", fmt.Errorf("object content is larger than %d bytes", maxUploadBytes)
	}
	contentType := strings.TrimSpace(stringValue(input, "content_type"))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	input["key"] = key
	input["content_type"] = contentType
	input["content_bytes"] = len(contentBytes)
	input["overwrite"] = boolValue(input, "overwrite")
	expectedETag, err := normalizeOptionalETag(input)
	if err != nil {
		return "", err
	}
	input["expected_etag"] = expectedETag
	if input["expected_etag"] != "" && !input["overwrite"].(bool) {
		return "", fmt.Errorf("expected_etag requires overwrite=true")
	}
	delete(input, "content_text")
	delete(input, "content_base64")
	if contentBase64 != "" {
		input["content_base64"] = contentBase64
	} else {
		input["content_base64"] = base64.StdEncoding.EncodeToString(contentBytes)
	}
	return fmt.Sprintf("%s (%d bytes)", key, len(contentBytes)), nil
}

func finalizePreparedAction(req connectors.ActionRequest, input map[string]any, risk connectors.RiskLevel, title, summary string) (connectors.PreparedAction, error) {
	if len(req.Reason) > maxS3ReasonBytes {
		return connectors.PreparedAction{}, fmt.Errorf("reason is too large")
	}
	trustedConditions := s3TrustConditionalRequests(req.Target)
	if s3ActionRequiresTrustedConditions(req.ActionName, input) && !trustedConditions {
		return connectors.PreparedAction{}, fmt.Errorf("%s requires verified conditional requests for this S3 provider", req.ActionName)
	}
	preview := copyMap(input)
	if _, ok := preview["content_base64"]; ok {
		preview["content_base64"] = fmt.Sprintf("[base64 content: %v bytes]", input["content_bytes"])
	}
	prepared := connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Dependencies:  connectors.NetworkTransportDependencies(req.Target),
		Risk:          risk,
		Title:         title,
		Summary:       summary,
		Preview:       preview,
		Payload:       input,
		ContextMaterial: map[string]any{
			"target":                     req.Target.Name,
			"profile":                    req.Profile.Label,
			"bucket":                     s3Bucket(req.Target),
			"connection_mode":            connectionMode(req.Target),
			"trust_conditional_requests": s3TrustConditionalRequests(req.Target),
		},
	}
	switch req.ActionName {
	case ActionUploadObject:
		if trustedConditions && !boolValue(input, "overwrite") {
			prepared.RetryPolicy = connectors.ConditionalRetryPolicy("overwrite")
		}
	case ActionDeleteObject:
		if trustedConditions && stringValue(input, "expected_etag") != "" {
			prepared.RetryPolicy = connectors.ConditionalRetryPolicy("expected_etag")
		}
	case ActionRestoreVersion:
		if trustedConditions && boolValue(input, "expected_current_absent") {
			prepared.RetryPolicy = connectors.ConditionalRetryPolicy("expected_current_absent")
		}
	case ActionDeleteVersion:
		prepared.RetryPolicy = &connectors.RetryPolicy{Class: connectors.RetryIdempotent}
	}
	return prepared, nil
}

func s3ActionRequiresTrustedConditions(actionName string, input map[string]any) bool {
	switch actionName {
	case ActionUploadObject:
		return !boolValue(input, "overwrite") || stringValue(input, "expected_etag") != ""
	case ActionDeleteObject:
		return stringValue(input, "expected_etag") != ""
	case ActionPresignUpload:
		return !boolValue(input, "overwrite")
	case ActionRestoreVersion:
		return true
	default:
		return false
	}
}

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if action.ActionName == ActionRenameObject {
		return connectors.ActionResult{}, errors.New("S3 rename_object is disabled because object stores do not provide an atomic cross-key move")
	}
	if s3ActionRequiresTrustedConditions(action.ActionName, action.Payload) && !s3TrustConditionalRequests(runtime.Target) {
		return connectors.ActionResult{}, fmt.Errorf("%s requires verified conditional requests for this S3 provider", action.ActionName)
	}
	client, err := newS3Client(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	switch action.ActionName {
	case ActionBucketInfo:
		return executeBucketInfo(ctx, client)
	case ActionListObjects:
		return executeListObjects(ctx, client, action.Payload)
	case ActionGetObjectMetadata:
		return executeGetObjectMetadata(ctx, client, action.Payload)
	case ActionDownloadObject:
		return executeDownloadObject(ctx, client, action.Payload)
	case ActionUploadObject:
		return executeUploadObject(ctx, client, action.Payload)
	case ActionDeleteObject:
		return executeDeleteObject(ctx, client, action.Payload)
	case ActionPresignDownload:
		return executePresignDownload(ctx, client, action.Payload)
	case ActionPresignUpload:
		return executePresignUpload(ctx, client, action.Payload)
	case ActionListVersions:
		return executeListObjectVersions(ctx, client, action.Payload)
	case ActionRestoreVersion:
		return executeRestoreObjectVersion(ctx, client, action.Payload)
	case ActionDeleteVersion:
		return executeDeleteObjectVersion(ctx, client, action.Payload)
	case ActionGetLifecycle:
		return executeGetBucketLifecycle(ctx, client)
	case ActionReplaceLifecycle:
		return executeReplaceBucketLifecycle(ctx, client, action.Payload)
	case ActionDeleteLifecycle:
		return executeDeleteBucketLifecycle(ctx, client)
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := newS3Client(ctx, runtime)
	if err != nil {
		return connectors.TestResult{Status: classifyS3TestError(err), Message: err.Error()}, nil
	}
	headers, err := client.HeadBucket(ctx)
	if err != nil {
		return connectors.TestResult{Status: classifyS3TestError(err), Message: err.Error()}, nil
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: "S3 bucket connection ok.",
		Details: map[string]any{
			"bucket":     client.bucket,
			"region":     client.region,
			"request_id": firstHeader(headers, "X-Amz-Request-Id", "X-Amz-Id-2"),
		},
	}, nil
}

func executeBucketInfo(ctx context.Context, client *s3Client) (connectors.ActionResult, error) {
	headers, err := client.HeadBucket(ctx)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	output := map[string]any{
		"bucket":   client.bucket,
		"region":   client.region,
		"endpoint": client.endpointDisplay(),
		"headers":  safeHeaders(headers),
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: fmt.Sprintf("Bucket %s is reachable at %s.", client.bucket, client.endpointDisplay()),
	}, nil
}

func executeListObjects(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	prefix := strings.TrimSpace(stringValue(input, "prefix"))
	search := strings.ToLower(strings.TrimSpace(stringValue(input, "search")))
	cursor := strings.TrimSpace(stringValue(input, "cursor"))
	limit := clampedInt(input, "limit", defaultS3ListLimit, 1, maxS3ListLimit)

	objects := make([]map[string]any, 0, limit)
	directories := make([]map[string]any, 0)
	nextCursor := cursor
	isTruncated := false
	scanned := 0
	scanLimited := false
	pageLimit := limit
	delimiter := search == ""
	if search != "" {
		pageLimit = maxS3ListLimit
	}

	for page := 0; page < maxS3SearchPages; page++ {
		result, err := client.ListObjects(ctx, prefix, nextCursor, pageLimit, delimiter)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		scanned += len(result.Contents)
		isTruncated = result.IsTruncated
		if search == "" {
			for _, directory := range result.CommonPrefixes {
				directories = append(directories, s3DirectorySummary(directory))
			}
		}
		for _, object := range result.Contents {
			if search != "" && !strings.Contains(strings.ToLower(object.Key), search) {
				continue
			}
			objects = append(objects, s3ObjectSummary(object))
		}
		nextCursor = result.NextContinuationToken
		if search == "" || len(objects) >= limit || !result.IsTruncated || nextCursor == "" {
			break
		}
		if page == maxS3SearchPages-1 {
			scanLimited = true
		}
	}

	if search != "" && len(objects) >= limit {
		isTruncated = nextCursor != ""
	}
	return s3ListResult(client.bucket, prefix, search, directories, objects, isTruncated, nextCursor, scanned, scanLimited), nil
}

func s3ObjectSummary(object s3Object) map[string]any {
	return map[string]any{
		"key":           object.Key,
		"size":          object.Size,
		"last_modified": object.LastModified,
		"etag":          strings.Trim(object.ETag, `"`),
		"storage_class": object.StorageClass,
	}
}

func s3DirectorySummary(directory s3CommonPrefix) map[string]any {
	return map[string]any{
		"prefix":       directory.Prefix,
		"name":         directoryName(directory.Prefix),
		"browse_input": map[string]any{"prefix": directory.Prefix, "limit": defaultS3ListLimit},
	}
}

func s3ListResult(bucket string, prefix string, search string, directories []map[string]any, objects []map[string]any, isTruncated bool, nextCursor string, scanned int, scanLimited bool) connectors.ActionResult {
	hints := s3ListAssistantHints(prefix, search, len(directories), len(objects), isTruncated, nextCursor, scanLimited)
	output := map[string]any{
		"bucket":          bucket,
		"prefix":          prefix,
		"search":          search,
		"directories":     directories,
		"directory_count": len(directories),
		"objects":         objects,
		"count":           len(objects),
		"is_truncated":    isTruncated,
		"next_cursor":     nextCursor,
		"scanned":         scanned,
		"scan_limited":    scanLimited,
		"assistant_hints": hints,
	}
	if isTruncated && nextCursor != "" {
		output["next_page_input"] = map[string]any{
			"prefix": prefix,
			"search": search,
			"cursor": nextCursor,
			"limit":  defaultS3ListLimit,
		}
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: fmt.Sprintf("%d folder(s), %d object(s)", len(directories), len(objects)),
	}
}

func s3ListAssistantHints(prefix string, search string, directoryCount int, objectCount int, isTruncated bool, nextCursor string, scanLimited bool) []string {
	hints := []string{}
	if directoryCount > 0 {
		hints = append(hints, "To enter a folder, call list_objects with that folder's browse_input.")
	}
	if objectCount > 0 {
		hints = append(hints, "Use get_object_metadata before download_object when you only need size, type, or headers.")
	}
	if isTruncated && nextCursor != "" {
		hints = append(hints, "More objects are available. Call list_objects with next_page_input or use next_cursor as cursor.")
	}
	if search != "" {
		hints = append(hints, "Search scans bounded pages and returns matching objects without folder grouping.")
	}
	if scanLimited {
		hints = append(hints, "Search stopped at the connector page limit; narrow the prefix or search term for a deeper lookup.")
	}
	if prefix == "" && search == "" && directoryCount == 0 && objectCount == 0 {
		hints = append(hints, "The bucket root is empty or the credential profile cannot see objects under this prefix.")
	}
	return hints
}

func executeGetObjectMetadata(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := normalizeObjectKey(input, "key")
	headers, err := client.HeadObject(ctx, key)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	output := objectMetadataOutput(client.bucket, key, headers)
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: fmt.Sprintf("%s · %s bytes", key, fmt.Sprint(output["content_length"])),
	}, nil
}

func executeDownloadObject(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := normalizeObjectKey(input, "key")
	maxBytes := clampedInt(input, "max_bytes", defaultDownloadMax, 1, maxDownloadBytes)
	data, headers, err := client.GetObject(ctx, key, maxBytes)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	output := map[string]any{
		"bucket":         client.bucket,
		"key":            key,
		"filename":       objectFilename(key),
		"content_type":   headers.Get("Content-Type"),
		"content_length": len(data),
		"content_base64": base64.StdEncoding.EncodeToString(data),
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: fmt.Sprintf("Downloaded %d byte(s) from %s.", len(data), key),
	}, nil
}

func executeUploadObject(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := normalizeObjectKey(input, "key")
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(stringValue(input, "content_base64")))
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("decode content_base64: %w", err)
	}
	if len(data) > maxUploadBytes {
		return connectors.ActionResult{}, fmt.Errorf("object content is larger than %d bytes", maxUploadBytes)
	}
	overwrite := boolValue(input, "overwrite")
	if !overwrite {
		if err := client.ensureObjectAbsent(ctx, key); err != nil {
			return connectors.ActionResult{}, err
		}
	}
	contentType := strings.TrimSpace(stringValue(input, "content_type"))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	headers := http.Header{}
	if !overwrite {
		headers.Set("If-None-Match", "*")
	} else if expectedETag, err := normalizeOptionalETag(input); err != nil {
		return connectors.ActionResult{}, err
	} else if expectedETag != "" {
		headers.Set("If-Match", quoteETag(expectedETag))
	}
	if err := client.PutObject(ctx, key, data, contentType, headers); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":       client.bucket,
			"key":          key,
			"bytes":        len(data),
			"content_type": contentType,
			"overwritten":  overwrite,
		},
		DisplayText: fmt.Sprintf("Uploaded %d byte(s) to %s.", len(data), key),
	}, nil
}

func executeDeleteObject(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := normalizeObjectKey(input, "key")
	headers := http.Header{}
	if expectedETag, err := normalizeOptionalETag(input); err != nil {
		return connectors.ActionResult{}, err
	} else if expectedETag != "" {
		headers.Set("If-Match", quoteETag(expectedETag))
	}
	if err := client.DeleteObject(ctx, key, headers); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":  client.bucket,
			"key":     key,
			"deleted": true,
		},
		DisplayText: fmt.Sprintf("Deleted %s.", key),
	}, nil
}

type s3Client struct {
	scheme       string
	host         string
	port         int
	region       string
	bucket       string
	pathStyle    bool
	accessKey    string
	secretKey    string
	sessionToken string
	httpClient   *http.Client
}

func newS3Client(ctx context.Context, runtime connectors.RuntimeContext) (*s3Client, error) {
	return newS3ClientWithTimeout(ctx, runtime, s3HTTPTimeout)
}

func newS3ClientWithTimeout(ctx context.Context, runtime connectors.RuntimeContext, timeout time.Duration) (*s3Client, error) {
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	accessKey := strings.TrimSpace(stringValue(runtime.Profile.Public, "access_key_id"))
	if accessKey == "" {
		return nil, fmt.Errorf("%w: access_key_id is required", ErrMissingSecret)
	}
	secretKey, err := runtime.Secrets.GetSecret(ctx, "secret_access_key")
	if err != nil || strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("%w: secret_access_key is required", ErrMissingSecret)
	}
	sessionToken, err := runtime.Secrets.GetSecret(ctx, "session_token")
	if err != nil && !errors.Is(err, connectors.ErrSecretNotFound) {
		return nil, fmt.Errorf("read session_token: %w", err)
	}
	client := &s3Client{
		scheme:       s3Scheme(runtime.Target),
		host:         s3Host(runtime.Target),
		port:         s3Port(runtime.Target),
		region:       s3Region(runtime.Target),
		bucket:       s3Bucket(runtime.Target),
		pathStyle:    s3PathStyle(runtime.Target),
		accessKey:    accessKey,
		secretKey:    strings.TrimSpace(secretKey),
		sessionToken: strings.TrimSpace(sessionToken),
	}
	if client.bucket == "" {
		return nil, fmt.Errorf("%w: bucket is required", ErrInvalidConfig)
	}
	request := connectors.NetworkDialRequest{
		SourceTargetRef:    runtime.Target.Ref,
		SourceProjectID:    runtime.Target.ProjectID,
		Mode:               connectionMode(runtime.Target),
		Host:               client.host,
		Port:               client.port,
		TransportTargetRef: strings.TrimSpace(stringValue(runtime.Target.Config, "transport_target_ref")),
	}
	client.httpClient = connectors.NewHTTPClient(transport, request, timeout)
	return client, nil
}

func (client *s3Client) HeadBucket(ctx context.Context) (http.Header, error) {
	_, headers, err := client.Do(ctx, http.MethodHead, "", nil, nil, maxS3ResponseBytes)
	return headers, err
}

func (client *s3Client) ListObjects(ctx context.Context, prefix string, token string, limit int, delimiter bool) (s3ListBucketResult, error) {
	query := url.Values{}
	query.Set("list-type", "2")
	query.Set("max-keys", strconv.Itoa(limit))
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	if delimiter {
		query.Set("delimiter", "/")
	}
	if token != "" {
		query.Set("continuation-token", token)
	}
	data, _, err := client.Do(ctx, http.MethodGet, "", query, nil, maxS3ResponseBytes)
	if err != nil {
		return s3ListBucketResult{}, err
	}
	var result s3ListBucketResult
	if err := xml.Unmarshal(data, &result); err != nil {
		return s3ListBucketResult{}, fmt.Errorf("decode s3 list response: %w", err)
	}
	return result, nil
}

func (client *s3Client) HeadObject(ctx context.Context, key string) (http.Header, error) {
	_, headers, err := client.Do(ctx, http.MethodHead, key, nil, nil, maxS3ResponseBytes)
	return headers, err
}

func (client *s3Client) ensureObjectAbsent(ctx context.Context, key string) error {
	if _, err := client.HeadObject(ctx, key); err == nil {
		return fmt.Errorf("object %q already exists; set overwrite=true to replace it", key)
	} else if !isNotFoundError(err) {
		return err
	}
	return nil
}

func (client *s3Client) GetObject(ctx context.Context, key string, maxBytes int) ([]byte, http.Header, error) {
	return client.Do(ctx, http.MethodGet, key, nil, nil, maxBytes)
}

func (client *s3Client) PutObject(ctx context.Context, key string, data []byte, contentType string, headers http.Header) error {
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", contentType)
	_, _, err := client.Do(ctx, http.MethodPut, key, nil, s3RequestBody{Headers: headers, Data: data}, maxS3ResponseBytes)
	return classifyS3MutationError(err, headers)
}

func (client *s3Client) DeleteObject(ctx context.Context, key string, headers http.Header) error {
	_, _, err := client.Do(ctx, http.MethodDelete, key, nil, s3RequestBody{Headers: headers}, maxS3ResponseBytes)
	return classifyS3MutationError(err, headers)
}

func normalizeOptionalETag(input map[string]any) (string, error) {
	return normalizeOptionalETagField(input, "expected_etag")
}

func normalizeOptionalETagField(input map[string]any, field string) (string, error) {
	value := strings.Trim(strings.TrimSpace(stringValue(input, field)), `"`)
	if len(value) == 0 {
		return "", nil
	}
	if len(value) > 1024 || value == "*" {
		return "", fmt.Errorf("%s is invalid", field)
	}
	for _, character := range []byte(value) {
		if character <= 0x20 || character == 0x7f || character == '"' {
			return "", fmt.Errorf("%s is invalid", field)
		}
	}
	return value, nil
}

func quoteETag(value string) string {
	return `"` + strings.Trim(value, `"`) + `"`
}

func (client *s3Client) Do(ctx context.Context, method string, key string, query url.Values, body any, limit int) ([]byte, http.Header, error) {
	var payload []byte
	headers := http.Header{}
	if requestBody, ok := body.(s3RequestBody); ok {
		payload = requestBody.Data
		headers = requestBody.Headers.Clone()
	} else if body != nil {
		return nil, nil, fmt.Errorf("unsupported s3 request body")
	}
	if headers == nil {
		headers = http.Header{}
	}
	u := client.URL(key, query)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	client.Sign(req, payload)
	req, requestDispatched := connectors.TrackHTTPRequestDispatch(req)
	resp, err := client.httpClient.Do(req)
	if err != nil {
		stage := "before_dispatch"
		if requestDispatched() {
			stage = "dispatch"
		}
		return nil, nil, &s3TransportError{stage: stage, err: err}
	}
	defer resp.Body.Close()
	readLimit := limit
	if readLimit <= 0 {
		readLimit = maxS3ResponseBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(readLimit)+1))
	if err != nil {
		return nil, nil, &s3TransportError{stage: "response_read", err: err}
	}
	if len(data) > readLimit {
		return nil, resp.Header, &s3TransportError{stage: "response_validation", err: fmt.Errorf("s3 response is larger than %d bytes", readLimit)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, s3HTTPError(resp.StatusCode, data)
	}
	return data, resp.Header, nil
}

func (client *s3Client) URL(key string, query url.Values) *url.URL {
	host := net.JoinHostPort(client.host, strconv.Itoa(client.port))
	if (client.scheme == "http" && client.port == 80) || (client.scheme == "https" && client.port == 443) {
		host = client.host
	}
	path := ""
	rawPath := ""
	if client.pathStyle {
		path = "/" + client.bucket
		rawPath = "/" + awsPathEscape(client.bucket)
		if key != "" {
			path += "/" + key
			rawPath += "/" + awsPathEscape(key)
		}
	} else {
		host = client.bucket + "." + host
		path = "/"
		rawPath = "/"
		if key != "" {
			path += key
			rawPath += awsPathEscape(key)
		}
	}
	u := &url.URL{Scheme: client.scheme, Host: host, Path: path}
	if rawPath != "" && rawPath != path {
		u.RawPath = rawPath
	}
	if len(query) > 0 {
		u.RawQuery = canonicalQuery(query)
	}
	return u
}

func (client *s3Client) Sign(req *http.Request, payload []byte) {
	client.signAt(req, payload, time.Now().UTC())
}

func (client *s3Client) signAt(req *http.Request, payload []byte, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if client.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", client.sessionToken)
	}
	canonicalRequest, signedHeaders := canonicalRequest(req, payloadHash)
	credentialScope := dateStamp + "/" + client.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := awsSigningKey(client.secretKey, dateStamp, client.region)
	signature := hmacSHA256Hex(signingKey, stringToSign)
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		client.accessKey,
		credentialScope,
		signedHeaders,
		signature,
	))
}

func (client *s3Client) endpointDisplay() string {
	return fmt.Sprintf("%s://%s", client.scheme, net.JoinHostPort(client.host, strconv.Itoa(client.port)))
}

type s3RequestBody struct {
	Headers http.Header
	Data    []byte
}

type s3ListBucketResult struct {
	XMLName               xml.Name         `xml:"ListBucketResult"`
	Name                  string           `xml:"Name"`
	Prefix                string           `xml:"Prefix"`
	KeyCount              int              `xml:"KeyCount"`
	MaxKeys               int              `xml:"MaxKeys"`
	IsTruncated           bool             `xml:"IsTruncated"`
	NextContinuationToken string           `xml:"NextContinuationToken"`
	Contents              []s3Object       `xml:"Contents"`
	CommonPrefixes        []s3CommonPrefix `xml:"CommonPrefixes"`
}

type s3Object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type s3CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

func canonicalRequest(req *http.Request, payloadHash string) (string, string) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           req.Header.Get("X-Amz-Date"),
	}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-amz-") || lower == "content-type" || lower == "if-match" || lower == "if-none-match" {
			headers[lower] = strings.Join(values, ",")
		}
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var canonicalHeaders strings.Builder
	for _, key := range keys {
		canonicalHeaders.WriteString(key)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(canonicalHeaderValue(headers[key]))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(keys, ";")
	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	return strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery(req.URL.Query()),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n"), signedHeaders
}

func awsSigningKey(secret string, dateStamp string, region string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	regionKey := hmacSHA256(dateKey, region)
	serviceKey := hmacSHA256(regionKey, "s3")
	return hmacSHA256(serviceKey, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

func hmacSHA256Hex(key []byte, data string) string {
	return hex.EncodeToString(hmacSHA256(key, data))
}

func sha256Hex(data []byte) string {
	if len(data) == 0 {
		return emptySHA256Hex
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0)
	for _, key := range keys {
		rawValues := append([]string(nil), values[key]...)
		sort.Strings(rawValues)
		for _, value := range rawValues {
			parts = append(parts, awsQueryEscape(key)+"="+awsQueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func awsPathEscape(value string) string {
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		segments[i] = awsQueryEscape(segment)
	}
	return strings.Join(segments, "/")
}

func awsQueryEscape(value string) string {
	escaped := url.QueryEscape(value)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func canonicalHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func objectMetadataOutput(bucket string, key string, headers http.Header) map[string]any {
	output := map[string]any{
		"bucket":         bucket,
		"key":            key,
		"content_type":   headers.Get("Content-Type"),
		"content_length": int64Header(headers, "Content-Length"),
		"etag":           strings.Trim(headers.Get("ETag"), `"`),
		"last_modified":  headers.Get("Last-Modified"),
		"metadata":       userMetadata(headers),
	}
	return output
}

func safeHeaders(headers http.Header) map[string]string {
	result := map[string]string{}
	for name, values := range headers {
		lower := strings.ToLower(name)
		if lower == "authorization" || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") {
			continue
		}
		if len(values) > 0 {
			result[name] = values[0]
		}
	}
	return result
}

func userMetadata(headers http.Header) map[string]string {
	result := map[string]string{}
	for name, values := range headers {
		if !strings.HasPrefix(strings.ToLower(name), "x-amz-meta-") || len(values) == 0 {
			continue
		}
		result[strings.TrimPrefix(strings.ToLower(name), "x-amz-meta-")] = values[0]
	}
	return result
}

func int64Header(headers http.Header, name string) int64 {
	value := strings.TrimSpace(headers.Get(name))
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func firstHeader(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func uploadBytes(contentText string, contentBase64 string) ([]byte, error) {
	if contentBase64 != "" {
		data, err := base64.StdEncoding.DecodeString(contentBase64)
		if err != nil {
			return nil, fmt.Errorf("decode content_base64: %w", err)
		}
		return data, nil
	}
	return []byte(contentText), nil
}
