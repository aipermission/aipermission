package s3connector

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
	key := normalizeObjectKey(input, "key")
	if _, err := client.HeadObject(ctx, key); err != nil {
		return connectors.ActionResult{}, err
	}
	return presignedActionResult(client, http.MethodGet, key, clampedInt(input, "expires_seconds", defaultPresignedExpirySeconds, minPresignedExpirySeconds, maxPresignedExpirySeconds), nil, time.Now().UTC())
}

func executePresignUpload(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	key := normalizeObjectKey(input, "key")
	requiredHeaders := map[string]string{}
	if !boolValue(input, "overwrite") {
		if err := client.ensureObjectAbsent(ctx, key); err != nil {
			return connectors.ActionResult{}, err
		}
		requiredHeaders["If-None-Match"] = "*"
	}
	return presignedActionResult(client, http.MethodPut, key, clampedInt(input, "expires_seconds", defaultPresignedExpirySeconds, minPresignedExpirySeconds, maxPresignedExpirySeconds), requiredHeaders, time.Now().UTC())
}

func presignedActionResult(client *s3Client, method string, key string, expiresSeconds int, requiredHeaders map[string]string, now time.Time) (connectors.ActionResult, error) {
	signedURL, expiresAt, err := client.presignObject(method, key, expiresSeconds, requiredHeaders, now)
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
			"bucket":           client.bucket,
			"key":              key,
			"operation":        operation,
			"method":           method,
			"url":              signedURL,
			"expires_at":       expiresAt.Format(time.RFC3339),
			"expires_seconds":  expiresSeconds,
			"required_headers": requiredHeaders,
			"warning":          "This URL is a temporary bearer credential. Share it only with the intended recipient.",
		},
		DisplayText: fmt.Sprintf("Created a %d-second presigned %s URL for %s.", expiresSeconds, operation, key),
	}, nil
}

func (client *s3Client) PresignObject(method string, key string, expiresSeconds int, now time.Time) (string, time.Time, error) {
	return client.presignObject(method, key, expiresSeconds, nil, now)
}

func (client *s3Client) presignObject(method string, key string, expiresSeconds int, requiredHeaders map[string]string, now time.Time) (string, time.Time, error) {
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
	if client.sessionToken != "" {
		return "", time.Time{}, fmt.Errorf("presigned URLs are unavailable for temporary-session credentials because returning the signed URL would disclose the session token")
	}
	return client.buildPresignedObjectUnchecked(method, key, expiresSeconds, requiredHeaders, now)
}

func (client *s3Client) buildPresignedObjectUnchecked(method string, key string, expiresSeconds int, requiredHeaders map[string]string, now time.Time) (string, time.Time, error) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	credentialScope := dateStamp + "/" + client.region + "/s3/aws4_request"
	canonicalHeaderValues := map[string]string{"host": canonicalHeaderValue(client.URL(key, nil).Host)}
	for name, value := range requiredHeaders {
		normalizedName := strings.ToLower(strings.TrimSpace(name))
		if normalizedName == "" || normalizedName == "host" {
			continue
		}
		canonicalHeaderValues[normalizedName] = canonicalHeaderValue(value)
	}
	signedHeaderNames := sortedHeaderNames(canonicalHeaderValues)
	var canonicalHeaders strings.Builder
	for _, name := range signedHeaderNames {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(canonicalHeaderValues[name])
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(signedHeaderNames, ";")
	query := url.Values{
		"X-Amz-Algorithm":     []string{"AWS4-HMAC-SHA256"},
		"X-Amz-Credential":    []string{client.accessKey + "/" + credentialScope},
		"X-Amz-Date":          []string{amzDate},
		"X-Amz-Expires":       []string{strconv.Itoa(expiresSeconds)},
		"X-Amz-SignedHeaders": []string{signedHeaders},
	}
	if client.sessionToken != "" {
		query.Set("X-Amz-Security-Token", client.sessionToken)
	}
	u := client.URL(key, query)
	canonicalRequest := strings.Join([]string{
		method,
		u.EscapedPath(),
		canonicalQuery(u.Query()),
		canonicalHeaders.String(),
		signedHeaders,
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

func sortedHeaderNames(headers map[string]string) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}
