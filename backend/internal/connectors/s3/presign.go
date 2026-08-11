package s3connector

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	defaultPresignedExpirySeconds = 15 * 60
	minPresignedExpirySeconds     = 60
	maxPresignedExpirySeconds     = 60 * 60
	presignedPayloadHash          = "UNSIGNED-PAYLOAD"
)

func executePresignDownload(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := stringValue(input, "key")
	if _, err := client.HeadObject(ctx, key); err != nil {
		return connectors.ActionResult{}, err
	}
	return presignedActionResult(client, http.MethodGet, key, intValue(input, "expires_seconds"), time.Now().UTC())
}

func executePresignUpload(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := stringValue(input, "key")
	if !boolValue(input, "overwrite") {
		if _, err := client.HeadObject(ctx, key); err == nil {
			return connectors.ActionResult{}, fmt.Errorf("object %q already exists; set overwrite=true to replace it", key)
		} else if !isNotFoundError(err) {
			return connectors.ActionResult{}, err
		}
	}
	return presignedActionResult(client, http.MethodPut, key, intValue(input, "expires_seconds"), time.Now().UTC())
}

func presignedActionResult(client *s3Client, method string, key string, expiresSeconds int, now time.Time) (connectors.ActionResult, error) {
	signedURL, expiresAt, err := client.PresignObject(method, key, expiresSeconds, now)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	operation := "download"
	if method == http.MethodPut {
		operation = "upload"
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":          client.bucket,
			"key":             key,
			"operation":       operation,
			"method":          method,
			"url":             signedURL,
			"expires_at":      expiresAt.Format(time.RFC3339),
			"expires_seconds": expiresSeconds,
			"warning":         "This URL is a temporary bearer credential. Share it only with the intended recipient.",
		},
		DisplayText: fmt.Sprintf("Created a %d-second presigned %s URL for %s.", expiresSeconds, operation, key),
	}, nil
}

func (client *s3Client) PresignObject(method string, key string, expiresSeconds int, now time.Time) (string, time.Time, error) {
	if method != http.MethodGet && method != http.MethodPut {
		return "", time.Time{}, fmt.Errorf("unsupported presigned URL method")
	}
	key = normalizeObjectKey(map[string]any{"key": key}, "key")
	if key == "" {
		return "", time.Time{}, fmt.Errorf("key is required")
	}
	if expiresSeconds < minPresignedExpirySeconds || expiresSeconds > maxPresignedExpirySeconds {
		return "", time.Time{}, fmt.Errorf("presigned URL expiry must be between %d and %d seconds", minPresignedExpirySeconds, maxPresignedExpirySeconds)
	}
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	credentialScope := dateStamp + "/" + client.region + "/s3/aws4_request"
	query := url.Values{
		"X-Amz-Algorithm":     []string{"AWS4-HMAC-SHA256"},
		"X-Amz-Credential":    []string{client.accessKey + "/" + credentialScope},
		"X-Amz-Date":          []string{amzDate},
		"X-Amz-Expires":       []string{strconv.Itoa(expiresSeconds)},
		"X-Amz-SignedHeaders": []string{"host"},
	}
	if client.sessionToken != "" {
		query.Set("X-Amz-Security-Token", client.sessionToken)
	}
	u := client.URL(key, query)
	canonicalRequest := strings.Join([]string{
		method,
		u.EscapedPath(),
		canonicalQuery(u.Query()),
		"host:" + canonicalHeaderValue(u.Host) + "\n",
		"host",
		presignedPayloadHash,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hmacSHA256Hex(awsSigningKey(client.secretKey, dateStamp, client.region), stringToSign)
	query.Set("X-Amz-Signature", signature)
	u.RawQuery = canonicalQuery(query)
	return u.String(), now.Add(time.Duration(expiresSeconds) * time.Second), nil
}

func intValue(values map[string]any, name string) int {
	switch value := values[name].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return parsed
	}
}
