package s3connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	defaultVersionListLimit = 100
	maxVersionListLimit     = 1000
	maxVersionIDBytes       = 2048
)

type s3VersionListResult struct {
	XMLName             xml.Name          `xml:"ListVersionsResult"`
	IsTruncated         bool              `xml:"IsTruncated"`
	NextKeyMarker       string            `xml:"NextKeyMarker"`
	NextVersionIDMarker string            `xml:"NextVersionIdMarker"`
	Versions            []s3ObjectVersion `xml:"Version"`
	DeleteMarkers       []s3ObjectVersion `xml:"DeleteMarker"`
}

type s3ObjectVersion struct {
	Key          string `xml:"Key"`
	VersionID    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type s3VersionCursor struct {
	KeyMarker       string `json:"key_marker"`
	VersionIDMarker string `json:"version_id_marker"`
}

func executeListObjectVersions(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := stringValue(input, "key")
	cursor, err := decodeVersionCursor(stringValue(input, "cursor"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result, err := client.ListObjectVersions(ctx, key, cursor, intValue(input, "limit"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	items := make([]map[string]any, 0, len(result.Versions)+len(result.DeleteMarkers))
	for _, version := range result.Versions {
		if version.Key != key {
			continue
		}
		items = append(items, objectVersionOutput(version, false))
	}
	for _, marker := range result.DeleteMarkers {
		if marker.Key != key {
			continue
		}
		items = append(items, objectVersionOutput(marker, true))
	}
	sort.SliceStable(items, func(i, j int) bool {
		return stringValue(items[i], "last_modified") > stringValue(items[j], "last_modified")
	})
	nextCursor := ""
	if result.IsTruncated {
		nextCursor, err = encodeVersionCursor(s3VersionCursor{
			KeyMarker:       result.NextKeyMarker,
			VersionIDMarker: result.NextVersionIDMarker,
		})
		if err != nil {
			return connectors.ActionResult{}, err
		}
	}
	output := map[string]any{
		"bucket":      client.bucket,
		"key":         key,
		"count":       len(items),
		"versions":    items,
		"has_more":    nextCursor != "",
		"next_cursor": nextCursor,
	}
	if nextCursor != "" {
		output["next_page_input"] = map[string]any{"key": key, "cursor": nextCursor, "limit": intValue(input, "limit")}
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: fmt.Sprintf("Listed %d version record(s) for %s.", len(items), key),
	}, nil
}

func executeRestoreObjectVersion(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := stringValue(input, "key")
	versionID := stringValue(input, "version_id")
	if err := client.CopyObjectVersion(ctx, key, versionID); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":              client.bucket,
			"key":                 key,
			"restored_version_id": versionID,
			"restored":            true,
		},
		DisplayText: fmt.Sprintf("Restored version %s of %s as the current object.", versionID, key),
	}, nil
}

func executeDeleteObjectVersion(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := stringValue(input, "key")
	versionID := stringValue(input, "version_id")
	if err := client.DeleteObjectVersion(ctx, key, versionID); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":             client.bucket,
			"key":                key,
			"deleted_version_id": versionID,
			"deleted":            true,
		},
		DisplayText: fmt.Sprintf("Deleted version %s of %s.", versionID, key),
	}, nil
}

func (client *s3Client) ListObjectVersions(ctx context.Context, key string, cursor s3VersionCursor, limit int) (s3VersionListResult, error) {
	query := url.Values{}
	query.Set("versions", "")
	query.Set("prefix", key)
	query.Set("max-keys", strconv.Itoa(limit))
	if cursor.KeyMarker != "" {
		query.Set("key-marker", cursor.KeyMarker)
	}
	if cursor.VersionIDMarker != "" {
		query.Set("version-id-marker", cursor.VersionIDMarker)
	}
	data, _, err := client.Do(ctx, http.MethodGet, "", query, nil, maxS3ResponseBytes)
	if err != nil {
		return s3VersionListResult{}, err
	}
	var result s3VersionListResult
	if err := xml.Unmarshal(data, &result); err != nil {
		return s3VersionListResult{}, fmt.Errorf("decode s3 version list response: %w", err)
	}
	return result, nil
}

func (client *s3Client) CopyObjectVersion(ctx context.Context, key string, versionID string) error {
	headers := http.Header{}
	headers.Set("X-Amz-Copy-Source", "/"+awsPathEscape(client.bucket)+"/"+awsPathEscape(key)+"?versionId="+awsQueryEscape(versionID))
	_, _, err := client.Do(ctx, http.MethodPut, key, nil, s3RequestBody{Headers: headers}, maxS3ResponseBytes)
	return err
}

func (client *s3Client) DeleteObjectVersion(ctx context.Context, key string, versionID string) error {
	query := url.Values{"versionId": []string{versionID}}
	_, _, err := client.Do(ctx, http.MethodDelete, key, query, nil, maxS3ResponseBytes)
	return err
}

func objectVersionOutput(version s3ObjectVersion, deleteMarker bool) map[string]any {
	return map[string]any{
		"key":           version.Key,
		"version_id":    version.VersionID,
		"is_latest":     version.IsLatest,
		"delete_marker": deleteMarker,
		"last_modified": version.LastModified,
		"etag":          strings.Trim(version.ETag, `"`),
		"size":          version.Size,
		"storage_class": version.StorageClass,
	}
}

func normalizeVersionID(input map[string]any) (string, error) {
	versionID := strings.TrimSpace(stringValue(input, "version_id"))
	if versionID == "" {
		return "", fmt.Errorf("version_id is required")
	}
	if len(versionID) > maxVersionIDBytes {
		return "", fmt.Errorf("version_id is too large")
	}
	return versionID, nil
}

func encodeVersionCursor(cursor s3VersionCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode S3 version cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeVersionCursor(raw string) (s3VersionCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return s3VersionCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 8192 {
		return s3VersionCursor{}, fmt.Errorf("invalid S3 version cursor")
	}
	var cursor s3VersionCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return s3VersionCursor{}, fmt.Errorf("invalid S3 version cursor")
	}
	if len(cursor.KeyMarker) > maxVersionIDBytes || len(cursor.VersionIDMarker) > maxVersionIDBytes {
		return s3VersionCursor{}, fmt.Errorf("invalid S3 version cursor")
	}
	return cursor, nil
}
