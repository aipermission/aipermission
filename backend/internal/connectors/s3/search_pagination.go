package s3connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	s3SearchCursorPrefix   = "s3-search-v1."
	maxS3SearchCursorBytes = 16 << 10
)

type s3SearchCursor struct {
	Prefix    string `json:"prefix"`
	Search    string `json:"search"`
	PageToken string `json:"page_token,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

func executeSearchObjects(ctx context.Context, client *s3Client, prefix string, search string, cursor string, limit int) (connectors.ActionResult, error) {
	position, err := decodeS3SearchCursor(cursor, prefix, search)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	objects := make([]map[string]any, 0, limit)
	scanned := 0
	pageToken := position.PageToken
	offset := position.Offset

	for page := 0; page < maxS3SearchPages; page++ {
		result, err := client.ListObjects(ctx, prefix, pageToken, maxS3ListLimit, false)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		if offset > len(result.Contents) {
			return connectors.ActionResult{}, fmt.Errorf("invalid S3 search cursor offset")
		}
		for index := offset; index < len(result.Contents); index++ {
			object := result.Contents[index]
			scanned++
			if !strings.Contains(strings.ToLower(object.Key), search) {
				continue
			}
			objects = append(objects, s3ObjectSummary(object))
			if len(objects) == limit {
				nextCursor, hasMore, err := nextS3SearchCursor(prefix, search, pageToken, index+1, result)
				if err != nil {
					return connectors.ActionResult{}, err
				}
				return s3ListResult(client.bucket, prefix, search, nil, objects, hasMore, nextCursor, scanned, false, limit), nil
			}
		}
		if !result.IsTruncated || strings.TrimSpace(result.NextContinuationToken) == "" {
			return s3ListResult(client.bucket, prefix, search, nil, objects, false, "", scanned, false, limit), nil
		}
		pageToken = result.NextContinuationToken
		offset = 0
	}

	nextCursor, err := encodeS3SearchCursor(s3SearchCursor{Prefix: prefix, Search: search, PageToken: pageToken})
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return s3ListResult(client.bucket, prefix, search, nil, objects, true, nextCursor, scanned, true, limit), nil
}

func nextS3SearchCursor(prefix string, search string, pageToken string, nextOffset int, result s3ListBucketResult) (string, bool, error) {
	position := s3SearchCursor{Prefix: prefix, Search: search}
	if nextOffset < len(result.Contents) {
		position.PageToken = pageToken
		position.Offset = nextOffset
	} else if result.IsTruncated && strings.TrimSpace(result.NextContinuationToken) != "" {
		position.PageToken = result.NextContinuationToken
	} else {
		return "", false, nil
	}
	cursor, err := encodeS3SearchCursor(position)
	if err != nil {
		return "", false, err
	}
	return cursor, true, nil
}

func encodeS3SearchCursor(cursor s3SearchCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode S3 search cursor: %w", err)
	}
	return s3SearchCursorPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeS3SearchCursor(value string, prefix string, search string) (s3SearchCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return s3SearchCursor{Prefix: prefix, Search: search}, nil
	}
	if len(value) > maxS3SearchCursorBytes || !strings.HasPrefix(value, s3SearchCursorPrefix) {
		return s3SearchCursor{}, fmt.Errorf("invalid S3 search cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, s3SearchCursorPrefix))
	if err != nil {
		return s3SearchCursor{}, fmt.Errorf("invalid S3 search cursor")
	}
	var cursor s3SearchCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return s3SearchCursor{}, fmt.Errorf("invalid S3 search cursor")
	}
	if cursor.Prefix != prefix || cursor.Search != search || cursor.Offset < 0 || cursor.Offset > maxS3ListLimit || len(cursor.PageToken) > maxS3SearchCursorBytes {
		return s3SearchCursor{}, fmt.Errorf("invalid S3 search cursor scope")
	}
	return cursor, nil
}
