package backups

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const (
	ServiceProviderType = "aipermission_backup"
	ServiceProtocol     = "1"
	maxServiceJSONBytes = 1 << 20
)

type ServiceClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type ServiceInfo struct {
	Service         string   `json:"service"`
	Version         string   `json:"version"`
	ProtocolVersion string   `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
	MaxUploadBytes  int64    `json:"max_upload_bytes"`
	StorageSchema   int      `json:"storage_schema"`
}

type ServiceBackup struct {
	ID                   string `json:"id"`
	StreamID             string `json:"stream_id"`
	DatabaseName         string `json:"database_name"`
	SourceInstallationID string `json:"source_installation_id"`
	Filename             string `json:"filename"`
	SizeBytes            int64  `json:"size_bytes"`
	SHA256               string `json:"sha256"`
	CreatedAt            string `json:"created_at"`
}

type ServiceStream struct {
	ID           string         `json:"id"`
	DatabaseName string         `json:"database_name"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	BackupCount  int64          `json:"backup_count"`
	LatestBackup *ServiceBackup `json:"latest_backup,omitempty"`
}

type ServicePruneResult struct {
	StreamID     string `json:"stream_id"`
	KeepLatest   int    `json:"keep_latest"`
	DeletedCount int    `json:"deleted_count"`
}

type ServiceDeleteResult struct {
	StreamID     string   `json:"stream_id"`
	DeletedIDs   []string `json:"deleted_ids"`
	DeletedCount int      `json:"deleted_count"`
}

type servicePage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

type ServiceError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e ServiceError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("backup service returned HTTP %d", e.StatusCode)
}

func NewServiceClient(rawBaseURL, token string) (*ServiceClient, error) {
	baseURL, err := ValidateServiceURL(rawBaseURL)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || strings.ContainsAny(token, "\r\n\t ") {
		return nil, ValidationError("backup service token must contain at least 32 characters without whitespace")
	}
	return &ServiceClient{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("backup service redirects are not allowed")
			},
		},
	}, nil
}

func ValidateServiceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" {
		return "", ValidationError("backup service URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", ValidationError("backup service URL must not contain credentials, a path, query, or fragment")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" {
		if scheme != "http" || !isLoopbackHost(parsed.Hostname()) {
			return "", ValidationError("backup service URL must use HTTPS; plaintext HTTP is allowed only on loopback")
		}
	}
	parsed.Scheme = scheme
	parsed.Path = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *ServiceClient) Info(ctx context.Context) (ServiceInfo, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var response ServiceInfo
	if err := c.doJSON(requestCtx, http.MethodGet, "/v1/info", nil, false, &response); err != nil {
		return ServiceInfo{}, err
	}
	if response.Service != "aipermission-backup" || response.ProtocolVersion != ServiceProtocol {
		return ServiceInfo{}, ValidationError("backup service protocol is incompatible with this AIPermission version")
	}
	return response, nil
}

func (c *ServiceClient) ListStreams(ctx context.Context) ([]ServiceStream, error) {
	items, err := listServicePages[ServiceStream](ctx, c, "/v1/streams")
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !validServiceIdentifier(item.ID) || strings.TrimSpace(item.DatabaseName) == "" || len(item.DatabaseName) > 128 {
			return nil, errors.New("backup service returned invalid stream metadata")
		}
	}
	return items, nil
}

func (c *ServiceClient) ListBackups(ctx context.Context, streamID string) ([]ServiceBackup, error) {
	if !validServiceIdentifier(streamID) {
		return nil, ValidationError("backup stream id is invalid")
	}
	items, err := listServicePages[ServiceBackup](ctx, c, "/v1/streams/"+url.PathEscape(streamID)+"/backups")
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := validateServiceBackup(item, streamID, 0); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *ServiceClient) PruneBackups(ctx context.Context, streamID string, keepLatest int) (ServicePruneResult, error) {
	if !validServiceIdentifier(streamID) || keepLatest < 1 || keepLatest > 1000 {
		return ServicePruneResult{}, ValidationError("backup stream id or retention count is invalid")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var response ServicePruneResult
	if err := c.doJSON(requestCtx, http.MethodPost, "/v1/streams/"+url.PathEscape(streamID)+"/prune", map[string]int{"keep_latest": keepLatest}, true, &response); err != nil {
		return ServicePruneResult{}, err
	}
	if response.StreamID != streamID || response.KeepLatest != keepLatest || response.DeletedCount < 0 {
		return ServicePruneResult{}, errors.New("backup service returned invalid prune metadata")
	}
	return response, nil
}

func (c *ServiceClient) DeleteBackups(ctx context.Context, streamID string, backupIDs []string) (ServiceDeleteResult, error) {
	if !validServiceIdentifier(streamID) || len(backupIDs) < 1 || len(backupIDs) > 100 {
		return ServiceDeleteResult{}, ValidationError("backup stream id or selected version ids are invalid")
	}
	seen := make(map[string]struct{}, len(backupIDs))
	for _, id := range backupIDs {
		if !validServiceIdentifier(id) {
			return ServiceDeleteResult{}, ValidationError("selected backup version id is invalid")
		}
		if _, exists := seen[id]; exists {
			return ServiceDeleteResult{}, ValidationError("selected backup version ids must be unique")
		}
		seen[id] = struct{}{}
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var response ServiceDeleteResult
	payload := map[string][]string{"backup_ids": backupIDs}
	if err := c.doJSON(requestCtx, http.MethodPost, "/v1/streams/"+url.PathEscape(streamID)+"/backups/delete", payload, true, &response); err != nil {
		return ServiceDeleteResult{}, err
	}
	if response.StreamID != streamID || response.DeletedCount != len(backupIDs) || len(response.DeletedIDs) != len(backupIDs) {
		return ServiceDeleteResult{}, errors.New("backup service returned invalid deletion metadata")
	}
	deleted := make(map[string]struct{}, len(response.DeletedIDs))
	for _, id := range response.DeletedIDs {
		if _, expected := seen[id]; !expected {
			return ServiceDeleteResult{}, errors.New("backup service returned an unexpected deleted version id")
		}
		deleted[id] = struct{}{}
	}
	if len(deleted) != len(seen) {
		return ServiceDeleteResult{}, errors.New("backup service returned duplicate deleted version ids")
	}
	return response, nil
}

func listServicePages[T any](ctx context.Context, client *ServiceClient, endpoint string) ([]T, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	items := make([]T, 0)
	cursor := ""
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		pageURL := endpoint + "?limit=100"
		if cursor != "" {
			pageURL += "&cursor=" + url.QueryEscape(cursor)
		}
		var page servicePage[T]
		if err := client.doJSON(requestCtx, http.MethodGet, pageURL, nil, true, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			return items, nil
		}
		if page.NextCursor == cursor {
			return nil, errors.New("backup service returned a repeated cursor")
		}
		cursor = page.NextCursor
	}
	return nil, errors.New("backup service listing exceeded 100 pages")
}

func (c *ServiceClient) Upload(ctx context.Context, streamID, databaseName, sourceInstallationID, filePath string) (ServiceBackup, error) {
	if !validServiceIdentifier(streamID) || !validServiceIdentifier(sourceInstallationID) {
		return ServiceBackup{}, ValidationError("backup stream or source installation id is invalid")
	}
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" || len(databaseName) > 128 || strings.ContainsAny(databaseName, "\r\n") {
		return ServiceBackup{}, ValidationError("database name must contain 1 to 128 characters on one line")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return ServiceBackup{}, fmt.Errorf("open encrypted backup snapshot: %w", err)
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil {
		return ServiceBackup{}, fmt.Errorf("inspect encrypted backup snapshot: %w", err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Size() < 1 {
		return ServiceBackup{}, ValidationError("encrypted backup snapshot must be a non-empty regular file")
	}
	expectedSHA256, err := hashAndRewind(file)
	if err != nil {
		return ServiceBackup{}, fmt.Errorf("hash encrypted backup snapshot: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	request, err := c.request(requestCtx, http.MethodPost, "/v1/streams/"+url.PathEscape(streamID)+"/backups", file, true)
	if err != nil {
		return ServiceBackup{}, err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-AIPermission-Database-Name", databaseName)
	request.Header.Set("X-AIPermission-Source-Installation-ID", sourceInstallationID)
	request.ContentLength = fileInfo.Size()
	response, err := c.client.Do(request)
	if err != nil {
		return ServiceBackup{}, fmt.Errorf("backup service upload failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return ServiceBackup{}, decodeServiceError(response)
	}
	var backup ServiceBackup
	if err := decodeBoundedJSON(response.Body, &backup); err != nil {
		return ServiceBackup{}, fmt.Errorf("parse backup service upload response: %w", err)
	}
	if err := validateServiceBackup(backup, streamID, fileInfo.Size()); err != nil {
		return ServiceBackup{}, err
	}
	if !strings.EqualFold(backup.SHA256, expectedSHA256) {
		return ServiceBackup{}, errors.New("backup service checksum does not match the uploaded snapshot")
	}
	return backup, nil
}

func hashAndRewind(file *os.File) (string, error) {
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (c *ServiceClient) Download(ctx context.Context, streamID, backupID, targetPath string, maxBytes int64) (ServiceBackup, error) {
	if !validServiceIdentifier(streamID) || !validServiceIdentifier(backupID) {
		return ServiceBackup{}, ValidationError("backup stream or version id is invalid")
	}
	if maxBytes < 1 {
		return ServiceBackup{}, ValidationError("local import limit must be positive")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	request, err := c.request(requestCtx, http.MethodGet, "/v1/streams/"+url.PathEscape(streamID)+"/backups/"+url.PathEscape(backupID), nil, true)
	if err != nil {
		return ServiceBackup{}, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return ServiceBackup{}, fmt.Errorf("backup service download failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ServiceBackup{}, decodeServiceError(response)
	}
	if response.ContentLength > maxBytes && response.ContentLength >= 0 {
		return ServiceBackup{}, ValidationError("remote backup exceeds the local import limit")
	}
	output, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ServiceBackup{}, fmt.Errorf("create temporary remote backup: %w", err)
	}
	remove := true
	defer func() {
		_ = output.Close()
		if remove {
			_ = os.Remove(targetPath)
		}
	}()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(output, digest), io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return ServiceBackup{}, fmt.Errorf("download encrypted backup: %w", err)
	}
	if written > maxBytes {
		return ServiceBackup{}, ValidationError("remote backup exceeds the local import limit")
	}
	expectedSHA256 := strings.ToLower(strings.TrimSpace(response.Header.Get("X-AIPermission-SHA256")))
	actualSHA256 := hex.EncodeToString(digest.Sum(nil))
	if !validSHA256(expectedSHA256) || expectedSHA256 != actualSHA256 {
		return ServiceBackup{}, errors.New("remote backup checksum verification failed")
	}
	if err := output.Sync(); err != nil {
		return ServiceBackup{}, fmt.Errorf("flush downloaded backup: %w", err)
	}
	if err := output.Close(); err != nil {
		return ServiceBackup{}, fmt.Errorf("close downloaded backup: %w", err)
	}
	remove = false
	return ServiceBackup{
		ID:        strings.TrimSpace(response.Header.Get("X-AIPermission-Backup-ID")),
		StreamID:  streamID,
		Filename:  filenameFromDisposition(response.Header.Get("Content-Disposition")),
		SizeBytes: written,
		SHA256:    actualSHA256,
	}, nil
}

func (c *ServiceClient) doJSON(ctx context.Context, method, endpoint string, payload any, protocol bool, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := c.request(ctx, method, endpoint, body, protocol)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("backup service request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return decodeServiceError(response)
	}
	if err := decodeBoundedJSON(response.Body, target); err != nil {
		return fmt.Errorf("parse backup service response: %w", err)
	}
	return nil
}

func (c *ServiceClient) request(ctx context.Context, method, endpoint string, body io.Reader, protocol bool) (*http.Request, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	parsed.Path = path.Clean("/" + strings.TrimPrefix(endpoint, "/"))
	parsed.RawQuery = ""
	if index := strings.Index(endpoint, "?"); index >= 0 {
		parsed.Path = path.Clean("/" + strings.TrimPrefix(endpoint[:index], "/"))
		parsed.RawQuery = endpoint[index+1:]
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if protocol {
		request.Header.Set("X-AIPermission-Protocol-Version", ServiceProtocol)
	}
	return request, nil
}

func decodeServiceError(response *http.Response) error {
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = decodeBoundedJSON(response.Body, &payload)
	return ServiceError{StatusCode: response.StatusCode, Code: strings.TrimSpace(payload.Error.Code), Message: strings.TrimSpace(payload.Error.Message)}
}

func decodeBoundedJSON(reader io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxServiceJSONBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxServiceJSONBytes {
		return errors.New("backup service JSON response is too large")
	}
	return json.Unmarshal(data, target)
}

func validServiceIdentifier(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for index, char := range value {
		valid := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && (char == '.' || char == '_' || char == '-')
		if !valid {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateServiceBackup(item ServiceBackup, streamID string, expectedSize int64) error {
	createdAt, err := time.Parse(time.RFC3339Nano, item.CreatedAt)
	if err != nil || createdAt.IsZero() || !validServiceIdentifier(item.ID) || item.StreamID != streamID ||
		strings.TrimSpace(item.DatabaseName) == "" || len(item.DatabaseName) > 128 ||
		!validServiceIdentifier(item.SourceInstallationID) || strings.TrimSpace(item.Filename) == "" ||
		item.SizeBytes < 1 || !validSHA256(strings.ToLower(strings.TrimSpace(item.SHA256))) {
		return errors.New("backup service returned invalid backup metadata")
	}
	if expectedSize > 0 && item.SizeBytes != expectedSize {
		return errors.New("backup service returned a size that does not match the uploaded snapshot")
	}
	return nil
}

func filenameFromDisposition(value string) string {
	_, parameters, err := mime.ParseMediaType(value)
	if err == nil {
		filename := strings.TrimSpace(parameters["filename"])
		filename = strings.ReplaceAll(filename, "\\", "/")
		if filename != "" {
			return path.Base(filename)
		}
	}
	return ""
}
