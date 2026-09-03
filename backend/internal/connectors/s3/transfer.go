package s3connector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	multipartThreshold     = 16 << 20
	multipartPartSize      = 8 << 20
	multipartPartAttempts  = 3
	maxTransferObjectBytes = 512 << 20
)

var (
	ErrRemotePathNotFound = errors.New("s3 object or prefix not found")
	ErrTransferLimit      = errors.New("s3 transfer limit exceeded")
)

type TransferProgress func(transferred int64, total int64)

type TransferOptions struct {
	Progress TransferProgress
	Wait     func(context.Context) error
	MaxBytes int64
}

type TransferResult struct {
	Bytes          int64
	Size           int64
	ChecksumSHA256 string
	DurationMS     int64
}

type RemoteFileEntry struct {
	Name       string
	Path       string
	Type       string
	Size       int64
	ModifiedAt string
}

type RemotePathStatus struct {
	Exists bool
	Type   string
	Size   int64
}

type RemoteFilePage struct {
	Entries    []RemoteFileEntry
	NextCursor string
	HasMore    bool
}

func BrowseRemoteFiles(ctx context.Context, runtime connectors.RuntimeContext, remotePath string) ([]RemoteFileEntry, error) {
	page, err := BrowseRemoteFilesPage(ctx, runtime, remotePath, "")
	return page.Entries, err
}

func BrowseRemoteFilesPage(ctx context.Context, runtime connectors.RuntimeContext, remotePath string, cursor string) (RemoteFilePage, error) {
	client, err := newS3Client(ctx, runtime)
	if err != nil {
		return RemoteFilePage{}, err
	}
	prefix := directoryPrefix(remotePath)
	result, err := client.ListObjects(ctx, prefix, strings.TrimSpace(cursor), maxS3ListLimit, true)
	if err != nil {
		return RemoteFilePage{}, err
	}
	entries := make([]RemoteFileEntry, 0, len(result.CommonPrefixes)+len(result.Contents))
	for _, item := range result.CommonPrefixes {
		name := strings.TrimSuffix(strings.TrimPrefix(item.Prefix, prefix), "/")
		if name == "" {
			continue
		}
		entries = append(entries, RemoteFileEntry{Name: name, Path: virtualObjectPath(item.Prefix), Type: "directory"})
	}
	for _, item := range result.Contents {
		if item.Key == prefix || strings.HasSuffix(item.Key, "/") {
			continue
		}
		entries = append(entries, RemoteFileEntry{
			Name:       path.Base(item.Key),
			Path:       virtualObjectPath(item.Key),
			Type:       "file",
			Size:       item.Size,
			ModifiedAt: item.LastModified,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return RemoteFilePage{Entries: entries, NextCursor: result.NextContinuationToken, HasMore: result.IsTruncated && strings.TrimSpace(result.NextContinuationToken) != ""}, nil
}

func StatRemotePath(ctx context.Context, runtime connectors.RuntimeContext, remotePath string) (RemotePathStatus, error) {
	client, err := newS3Client(ctx, runtime)
	if err != nil {
		return RemotePathStatus{}, err
	}
	return statRemotePath(ctx, client, remotePath)
}

func statRemotePath(ctx context.Context, client *s3Client, remotePath string) (RemotePathStatus, error) {
	if cleanVirtualPath(remotePath) == "/" {
		return RemotePathStatus{Exists: true, Type: "directory"}, nil
	}
	key := objectKey(remotePath)
	if !strings.HasSuffix(remotePath, "/") {
		headers, headErr := client.HeadObject(ctx, key)
		switch {
		case headErr == nil:
			return RemotePathStatus{Exists: true, Type: "file", Size: int64Header(headers, "Content-Length")}, nil
		case !isNotFoundError(headErr):
			return RemotePathStatus{}, headErr
		}
	}
	prefix := strings.TrimSuffix(key, "/") + "/"
	result, err := client.ListObjects(ctx, prefix, "", 1, false)
	if err != nil {
		return RemotePathStatus{}, err
	}
	return RemotePathStatus{Exists: len(result.Contents) > 0, Type: "directory"}, nil
}

func ListRecursiveFiles(ctx context.Context, runtime connectors.RuntimeContext, remotePath string, maxItems int, maxObjectBytes int64, maxBatchBytes int64) ([]RemoteFileEntry, error) {
	if maxItems < 1 || maxObjectBytes < 1 || maxBatchBytes < 1 {
		return nil, fmt.Errorf("recursive transfer limits are required")
	}
	client, err := newS3Client(ctx, runtime)
	if err != nil {
		return nil, err
	}
	status, err := statRemotePath(ctx, client, remotePath)
	if err != nil {
		return nil, err
	}
	if !status.Exists {
		return nil, ErrRemotePathNotFound
	}
	if status.Type == "file" {
		if status.Size > maxObjectBytes {
			return nil, fmt.Errorf("%w: selected object exceeds the per-object byte limit", ErrTransferLimit)
		}
		return []RemoteFileEntry{{Name: path.Base(remotePath), Path: cleanVirtualPath(remotePath), Type: "file", Size: status.Size}}, nil
	}
	prefix := directoryPrefix(remotePath)
	entries := make([]RemoteFileEntry, 0)
	var total int64
	token := ""
	for {
		result, err := client.ListObjects(ctx, prefix, token, maxS3ListLimit, false)
		if err != nil {
			return nil, err
		}
		for _, item := range result.Contents {
			if strings.HasSuffix(item.Key, "/") {
				continue
			}
			if len(entries) >= maxItems {
				return nil, fmt.Errorf("%w: selected prefix exceeds the %d object limit", ErrTransferLimit, maxItems)
			}
			if item.Size > maxObjectBytes {
				return nil, fmt.Errorf("%w: object %q exceeds the per-object byte limit", ErrTransferLimit, item.Key)
			}
			total += item.Size
			if total > maxBatchBytes {
				return nil, fmt.Errorf("%w: selected prefix exceeds the %d byte limit", ErrTransferLimit, maxBatchBytes)
			}
			entries = append(entries, RemoteFileEntry{Name: path.Base(item.Key), Path: virtualObjectPath(item.Key), Type: "file", Size: item.Size, ModifiedAt: item.LastModified})
		}
		if !result.IsTruncated || strings.TrimSpace(result.NextContinuationToken) == "" {
			break
		}
		token = result.NextContinuationToken
	}
	return entries, nil
}

func UploadFile(ctx context.Context, runtime connectors.RuntimeContext, localPath string, remotePath string, overwrite bool, options TransferOptions) (TransferResult, error) {
	started := time.Now()
	if !overwrite && !s3TrustConditionalRequests(runtime.Target) {
		return TransferResult{}, fmt.Errorf("S3 no-overwrite upload requires verified conditional requests for this provider")
	}
	client, err := newS3ClientWithTimeout(ctx, runtime, 0)
	if err != nil {
		return TransferResult{}, err
	}
	key := objectKey(remotePath)
	if key == "" || strings.HasSuffix(key, "/") {
		return TransferResult{}, fmt.Errorf("remote path must identify an object")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return TransferResult{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return TransferResult{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxTransferObjectBytes {
		return TransferResult{}, fmt.Errorf("upload object must be a regular file no larger than %d bytes", maxTransferObjectBytes)
	}
	if !overwrite {
		if err := client.ensureObjectAbsent(ctx, key); err != nil {
			return TransferResult{}, err
		}
	}
	checksum, err := fileSHA256(file)
	if err != nil {
		return TransferResult{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return TransferResult{}, err
	}
	if err := waitTransfer(ctx, options); err != nil {
		return TransferResult{}, err
	}
	if info.Size() <= multipartThreshold {
		data, err := io.ReadAll(file)
		if err != nil {
			return TransferResult{}, err
		}
		contentType := mime.TypeByExtension(path.Ext(key))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		headers := http.Header{}
		if !overwrite {
			headers.Set("If-None-Match", "*")
		}
		if err := client.PutObject(ctx, key, data, contentType, headers); err != nil {
			return TransferResult{}, err
		}
		progressTransfer(options, info.Size(), info.Size())
	} else if err := client.multipartUpload(ctx, key, file, info.Size(), !overwrite, options); err != nil {
		return TransferResult{}, err
	}
	return TransferResult{Bytes: info.Size(), Size: info.Size(), ChecksumSHA256: checksum, DurationMS: time.Since(started).Milliseconds()}, nil
}

func DownloadFile(ctx context.Context, runtime connectors.RuntimeContext, remotePath string, localPath string, options TransferOptions) (TransferResult, error) {
	started := time.Now()
	client, err := newS3ClientWithTimeout(ctx, runtime, 0)
	if err != nil {
		return TransferResult{}, err
	}
	key := objectKey(remotePath)
	if key == "" || strings.HasSuffix(key, "/") {
		return TransferResult{}, fmt.Errorf("remote path must identify an object")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.URL(key, nil).String(), nil)
	if err != nil {
		return TransferResult{}, err
	}
	client.Sign(req, nil)
	response, err := client.httpClient.Do(req)
	if err != nil {
		return TransferResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, maxS3ResponseBytes))
		return TransferResult{}, s3HTTPError(response.StatusCode, data)
	}
	maxBytes := int64(maxTransferObjectBytes)
	if options.MaxBytes > 0 && options.MaxBytes < maxBytes {
		maxBytes = options.MaxBytes
	}
	if response.ContentLength > maxBytes {
		return TransferResult{}, fmt.Errorf("download object is larger than %d bytes", maxBytes)
	}
	output, err := os.OpenFile(localPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return TransferResult{}, err
	}
	completed := false
	defer func() {
		_ = output.Close()
		if !completed {
			_ = os.Remove(localPath)
		}
	}()
	hash := sha256.New()
	buffer := make([]byte, 256<<10)
	var written int64
	for {
		if err := waitTransfer(ctx, options); err != nil {
			return TransferResult{}, err
		}
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			written += int64(read)
			if written > maxBytes {
				return TransferResult{}, fmt.Errorf("download object is larger than %d bytes", maxBytes)
			}
			if _, err := output.Write(buffer[:read]); err != nil {
				return TransferResult{}, err
			}
			_, _ = hash.Write(buffer[:read])
			progressTransfer(options, written, response.ContentLength)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return TransferResult{}, readErr
		}
	}
	if err := output.Close(); err != nil {
		return TransferResult{}, err
	}
	completed = true
	return TransferResult{Bytes: written, Size: written, ChecksumSHA256: hex.EncodeToString(hash.Sum(nil)), DurationMS: time.Since(started).Milliseconds()}, nil
}

func (client *s3Client) multipartUpload(ctx context.Context, key string, file *os.File, size int64, preventOverwrite bool, options TransferOptions) (err error) {
	query := url.Values{"uploads": []string{""}}
	data, _, err := client.Do(ctx, http.MethodPost, key, query, s3RequestBody{Headers: http.Header{}, Data: nil}, maxS3ResponseBytes)
	if err != nil {
		return classifyMultipartInitiationError(err)
	}
	var initiated struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.Unmarshal(data, &initiated); err != nil || strings.TrimSpace(initiated.UploadID) == "" {
		return unknownMultipartInitiationError("response_validation", fmt.Errorf("decode multipart upload response"))
	}
	uploadID := initiated.UploadID
	completed := false
	defer func() {
		if !completed {
			abortCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			abortQuery := url.Values{"uploadId": []string{uploadID}}
			if _, _, abortErr := client.Do(abortCtx, http.MethodDelete, key, abortQuery, nil, maxS3ResponseBytes); abortErr != nil {
				err = errors.Join(err, fmt.Errorf("abort incomplete multipart upload: %w", abortErr))
			}
		}
	}()
	type completedPart struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	}
	parts := make([]completedPart, 0, int((size+multipartPartSize-1)/multipartPartSize))
	var transferred int64
	for partNumber := 1; transferred < size; partNumber++ {
		if err := waitTransfer(ctx, options); err != nil {
			return err
		}
		remaining := size - transferred
		partBytes := int64(multipartPartSize)
		if remaining < partBytes {
			partBytes = remaining
		}
		partData := make([]byte, int(partBytes))
		if _, err := io.ReadFull(file, partData); err != nil {
			return err
		}
		partQuery := url.Values{
			"partNumber": []string{strconv.Itoa(partNumber)},
			"uploadId":   []string{uploadID},
		}
		var headers http.Header
		var uploadErr error
		// UploadPart replaces the same upload ID and part number, so this bounded
		// internal retry converges on one remote part instead of duplicating work.
		for attempt := 1; attempt <= multipartPartAttempts; attempt++ {
			_, headers, uploadErr = client.Do(ctx, http.MethodPut, key, partQuery, s3RequestBody{Headers: http.Header{}, Data: partData}, maxS3ResponseBytes)
			if uploadErr == nil || !connectors.RetryableIdempotentOperationError(uploadErr) {
				break
			}
			if attempt < multipartPartAttempts {
				if err := waitRetry(ctx, attempt); err != nil {
					return err
				}
			}
		}
		if uploadErr != nil {
			return fmt.Errorf("upload multipart part %d: %w", partNumber, uploadErr)
		}
		etag := strings.TrimSpace(headers.Get("ETag"))
		if etag == "" {
			return fmt.Errorf("multipart part %d response omitted ETag", partNumber)
		}
		parts = append(parts, completedPart{PartNumber: partNumber, ETag: etag})
		transferred += partBytes
		progressTransfer(options, transferred, size)
	}
	completePayload, err := xml.Marshal(struct {
		XMLName xml.Name        `xml:"CompleteMultipartUpload"`
		Parts   []completedPart `xml:"Part"`
	}{Parts: parts})
	if err != nil {
		return err
	}
	completeQuery := url.Values{"uploadId": []string{uploadID}}
	completeHeaders := http.Header{"Content-Type": []string{"application/xml"}}
	if preventOverwrite {
		completeHeaders.Set("If-None-Match", "*")
	}
	completeData, _, err := client.Do(ctx, http.MethodPost, key, completeQuery, s3RequestBody{Headers: completeHeaders, Data: completePayload}, maxS3ResponseBytes)
	if err != nil {
		return classifyS3MutationError(err, completeHeaders)
	}
	if err := validateMultipartCompletionResponse(completeData); err != nil {
		return err
	}
	completed = true
	return nil
}

func classifyMultipartInitiationError(err error) error {
	classified := classifyS3MutationError(err, nil)
	if connectors.ErrorStatus(classified) != connectors.ResultOutcomeUnknown {
		return classified
	}
	return unknownMultipartInitiationError("request_or_response", classified)
}

func unknownMultipartInitiationError(stage string, err error) error {
	return connectors.ClassifyActionError(
		"outcome_unknown",
		connectors.ResultOutcomeUnknown,
		map[string]any{"dispatch_stage": stage, "cleanup_required": true},
		fmt.Errorf("multipart upload initiation outcome is unknown; inspect or clean up incomplete uploads before retrying: %w", err),
	)
}

func validateMultipartCompletionResponse(data []byte) error {
	var response struct {
		XMLName xml.Name
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
		ETag    string `xml:"ETag"`
	}
	if err := xml.Unmarshal(data, &response); err != nil {
		return unknownS3MutationError("response_validation", fmt.Errorf("decode s3 multipart completion response: %w", err))
	}
	if response.XMLName.Local == "Error" || strings.TrimSpace(response.Code) != "" {
		return classifyS3ServiceError(http.StatusOK, response.Code, response.Message)
	}
	if response.XMLName.Local != "CompleteMultipartUploadResult" || strings.TrimSpace(response.ETag) == "" {
		return unknownS3MutationError("response_validation", fmt.Errorf("s3 multipart completion response did not confirm completion"))
	}
	return nil
}

func fileSHA256(file *os.File) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func waitTransfer(ctx context.Context, options TransferOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.Wait != nil {
		return options.Wait(ctx)
	}
	return nil
}

func waitRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func progressTransfer(options TransferOptions, transferred int64, total int64) {
	if options.Progress != nil {
		options.Progress(transferred, total)
	}
}

func cleanVirtualPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimLeft(value, "/"))
}

func objectKey(remotePath string) string {
	return strings.TrimLeft(cleanVirtualPath(remotePath), "/")
}

func directoryPrefix(remotePath string) string {
	key := objectKey(remotePath)
	if key == "" {
		return ""
	}
	return strings.TrimSuffix(key, "/") + "/"
}

func virtualObjectPath(key string) string {
	if strings.TrimSpace(key) == "" {
		return "/"
	}
	return "/" + strings.TrimLeft(key, "/")
}
