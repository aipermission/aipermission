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

type s3Client struct {
	scheme            string
	host              string
	port              int
	region            string
	bucket            string
	pathStyle         bool
	accessKey         string
	secretKey         string
	sessionToken      string
	httpClient        *http.Client
	registerSensitive func(string)
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
		registerSensitive: func(value string) {
			connectors.RegisterSensitiveValue(runtime.Secrets, value)
		},
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
	authorization := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		client.accessKey,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Header.Set("Authorization", authorization)
	if client.registerSensitive != nil {
		client.registerSensitive(authorization)
	}
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
