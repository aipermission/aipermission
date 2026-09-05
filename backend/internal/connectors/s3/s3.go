// Package s3connector defines the S3-compatible storage connector contract.
package s3connector

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
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
		prefix := stringValue(input, "prefix")
		search := strings.TrimSpace(stringValue(input, "search"))
		cursor := strings.TrimSpace(stringValue(input, "cursor"))
		if search != "" {
			if _, err := decodeS3SearchCursor(cursor, prefix, strings.ToLower(search)); err != nil {
				return connectors.PreparedAction{}, err
			}
		}
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
	prefix := stringValue(input, "prefix")
	search := strings.ToLower(strings.TrimSpace(stringValue(input, "search")))
	cursor := strings.TrimSpace(stringValue(input, "cursor"))
	limit := clampedInt(input, "limit", defaultS3ListLimit, 1, maxS3ListLimit)
	if search != "" {
		return executeSearchObjects(ctx, client, prefix, search, cursor, limit)
	}

	result, err := client.ListObjects(ctx, prefix, cursor, limit, true)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	directories := make([]map[string]any, 0, len(result.CommonPrefixes))
	for _, directory := range result.CommonPrefixes {
		directories = append(directories, s3DirectorySummary(directory))
	}
	objects := make([]map[string]any, 0, len(result.Contents))
	for _, object := range result.Contents {
		objects = append(objects, s3ObjectSummary(object))
	}
	return s3ListResult(client.bucket, prefix, search, directories, objects, result.IsTruncated, result.NextContinuationToken, len(result.Contents), false, limit), nil
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

func s3ListResult(bucket string, prefix string, search string, directories []map[string]any, objects []map[string]any, isTruncated bool, nextCursor string, scanned int, scanLimited bool, limit int) connectors.ActionResult {
	if directories == nil {
		directories = []map[string]any{}
	}
	if objects == nil {
		objects = []map[string]any{}
	}
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
			"limit":  limit,
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
