package filetransfer

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

func normalizeCreateRequest(request CreateRequest) (CreateRequest, error) {
	request.Direction = strings.TrimSpace(request.Direction)
	request.Source = strings.TrimSpace(request.Source)
	request.LocalPath = strings.TrimSpace(request.LocalPath)
	request.RemotePath = strings.TrimSpace(request.RemotePath)
	request.FileName = strings.TrimSpace(request.FileName)
	request.TempPath = strings.TrimSpace(request.TempPath)
	if request.Source == "" {
		request.Source = SourceUI
	}
	if request.RuntimeID < 1 {
		return request, fmt.Errorf("runtime_id is required")
	}
	if request.Direction != DirectionUpload && request.Direction != DirectionDownload {
		return request, fmt.Errorf("direction must be upload or download")
	}
	if request.Source != SourceUI && request.Source != SourceMCP {
		return request, fmt.Errorf("source must be ui or mcp")
	}
	if request.BatchID < 0 {
		return request, fmt.Errorf("batch_id cannot be negative")
	}
	if request.QueueIndex < 0 {
		return request, fmt.Errorf("queue_index cannot be negative")
	}
	if err := validatePathLike("local_path", request.LocalPath, false); err != nil {
		return request, err
	}
	if err := validatePathLike("remote_path", request.RemotePath, true); err != nil {
		return request, err
	}
	if err := validatePathLike("file_name", request.FileName, false); err != nil {
		return request, err
	}
	if request.SizeBytes < 0 {
		return request, fmt.Errorf("size_bytes cannot be negative")
	}
	if request.TransferredBytes < 0 {
		return request, fmt.Errorf("transferred_bytes cannot be negative")
	}
	return request, nil
}

func normalizeBatchCreateRequest(request CreateBatchRequest) (CreateBatchRequest, error) {
	request.Direction = strings.TrimSpace(request.Direction)
	request.Source = strings.TrimSpace(request.Source)
	request.Status = strings.TrimSpace(request.Status)
	request.ApprovalNote = strings.TrimSpace(request.ApprovalNote)
	request.ArchiveName = strings.TrimSpace(request.ArchiveName)
	if request.Source == "" {
		request.Source = SourceUI
	}
	if request.Status == "" {
		request.Status = StatusPending
	}
	if request.RuntimeID < 1 {
		return request, fmt.Errorf("runtime_id is required")
	}
	if request.Direction != DirectionUpload && request.Direction != DirectionDownload {
		return request, fmt.Errorf("direction must be upload or download")
	}
	if request.Source != SourceUI && request.Source != SourceMCP {
		return request, fmt.Errorf("source must be ui or mcp")
	}
	if request.Status != StatusPending && request.Status != StatusPendingApproval {
		return request, fmt.Errorf("status must be pending or pending_approval")
	}
	if err := validatePathLike("approval_note", request.ApprovalNote, false); err != nil {
		return request, err
	}
	if len(request.Items) == 0 {
		return request, fmt.Errorf("at least one file transfer item is required")
	}
	if len(request.Items) > 100 {
		return request, fmt.Errorf("file transfer batch cannot contain more than 100 items")
	}
	if err := validatePathLike("archive_name", request.ArchiveName, false); err != nil {
		return request, err
	}
	for i := range request.Items {
		request.Items[i].BatchID = 0
		request.Items[i].QueueIndex = i
		request.Items[i].RuntimeID = request.RuntimeID
		request.Items[i].Direction = request.Direction
		request.Items[i].Source = request.Source
		normalized, err := normalizeCreateRequest(request.Items[i])
		if err != nil {
			return request, fmt.Errorf("item %d: %w", i+1, err)
		}
		request.Items[i] = normalized
	}
	return request, nil
}

func normalizeListFilter(filter ListFilter) ListFilter {
	filter.Direction = strings.TrimSpace(filter.Direction)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.TargetIDs = normalizeTargetIDs(filter.TargetIDs)
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	switch filter.Direction {
	case DirectionUpload, DirectionDownload:
	default:
		filter.Direction = ""
	}
	switch filter.Status {
	case StatusPending, StatusPendingApproval, StatusRunning, StatusPaused, StatusCompleted, StatusFailed, StatusCanceled:
	default:
		filter.Status = ""
	}
	return filter
}

func normalizeBatchListFilter(filter BatchListFilter) BatchListFilter {
	filter.Direction = strings.TrimSpace(filter.Direction)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.TargetIDs = normalizeTargetIDs(filter.TargetIDs)
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	switch filter.Direction {
	case DirectionUpload, DirectionDownload:
	default:
		filter.Direction = ""
	}
	switch filter.Status {
	case StatusPending, StatusPendingApproval, StatusRunning, StatusPaused, StatusCompleted, StatusFailed, StatusCanceled:
	default:
		filter.Status = ""
	}
	return filter
}

func normalizeTargetIDs(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := map[int64]bool{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value < 1 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func validatePathLike(field string, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if len([]rune(value)) > maxPathRunes {
		return fmt.Errorf("%s must be %d characters or fewer", field, maxPathRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s cannot contain control characters", field)
		}
	}
	return nil
}

func nullableBatchID(id int64) any {
	if id < 1 {
		return nil
	}
	return id
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeApprovedItemIDs(values []int64) ([]int64, error) {
	seen := map[int64]bool{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value < 1 || seen[value] {
			return nil, ErrInvalidArgument
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func rejectionNote(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return "rejected by local user"
	}
	return note
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
