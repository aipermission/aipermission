package transferruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

var errFileTransferTimedOut = errors.New("file transfer timed out")

func (s Runner) failFileTransfer(runtime *Runtime, transferID int64, execution *Execution, err error, failureKind string) bool {
	message := "file transfer failed"
	if execution != nil {
		message = execution.Boundary.Redact(fileTransferFailureMessage(err))
	}
	return s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.store.FailWithDetails(ctx, transferID, message, failureKind, safeTransferFailureDetails(execution, err))
	})
}

func safeTransferFailureDetails(execution *Execution, err error) map[string]any {
	if execution == nil {
		return nil
	}
	source := connectors.ErrorDetails(err)
	if len(source) == 0 {
		return nil
	}
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := transferFailureDetailPriority(keys[i]), transferFailureDetailPriority(keys[j])
		if left != right {
			return left < right
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 8 {
		keys = keys[:8]
	}
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" || cleanKey == "failure_kind" || len(cleanKey) > 64 {
			continue
		}
		switch value := source[key].(type) {
		case string:
			value = execution.Boundary.Redact(value)
			appendBoundedTransferFailureText(result, cleanKey, boundTransferFailureText(value, 18*1024))
		case bool, float64, int, int64:
			result[cleanKey] = value
			if !transferFailureDetailsFit(result) {
				delete(result, cleanKey)
			}
		}
	}
	return result
}

func transferFailureDetailPriority(key string) int {
	switch strings.TrimSpace(key) {
	case "dispatch_stage":
		return 0
	case "retry_safe":
		return 1
	case "recovery_hint":
		return 2
	default:
		return 3
	}
}

func appendBoundedTransferFailureText(result map[string]any, key string, value string) {
	result[key] = value
	if transferFailureDetailsFit(result) {
		return
	}
	delete(result, key)
	low, high := 0, len(value)
	best := ""
	for low <= high {
		middle := low + (high-low)/2
		candidate := boundTransferFailureText(value, middle)
		result[key] = candidate
		if transferFailureDetailsFit(result) {
			best = candidate
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	result[key] = best
	if !transferFailureDetailsFit(result) {
		delete(result, key)
	}
}

func transferFailureDetailsFit(details map[string]any) bool {
	encoded, err := json.Marshal(details)
	return err == nil && len(encoded) <= filetransfer.MaxFailureDetailsJSONBytes
}

func boundTransferFailureText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (s Runner) finishFileTransferError(runtime *Runtime, transferID int64, ctx context.Context, execution *Execution, err error) bool {
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		return s.failFileTransfer(runtime, transferID, execution, err, filetransfer.FailureKindOutcomeUnknown)
	}
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		return s.failFileTransfer(runtime, transferID, execution, errFileTransferTimedOut, filetransfer.FailureKindTimeout)
	case fileTransferCanceledByUser:
		return s.cancelFileTransferRecord(runtime, transferID, "canceled by local user")
	default:
		return s.failFileTransfer(runtime, transferID, execution, err, filetransfer.FailureKindUnknown)
	}
}

func (s Runner) finishFileTransferBatchError(runtime *Runtime, batchID int64, ctx context.Context, execution *Execution, err error) bool {
	message := "file transfer batch failed"
	if execution != nil {
		message = execution.Boundary.Redact(fileTransferFailureMessage(err))
	}
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindOutcomeUnknown)
		})
	}
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.FailBatchWithKind(ctx, batchID, "file transfer batch timed out", filetransfer.FailureKindTimeout)
		})
	case fileTransferCanceledByUser:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.CancelBatch(ctx, batchID, "canceled by local user")
		})
	default:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindUnknown)
		})
	}
}

type fileTransferInterruption int

const (
	fileTransferNotInterrupted fileTransferInterruption = iota
	fileTransferCanceledByUser
	fileTransferTimedOut
)

func classifyFileTransferInterruption(ctx context.Context, err error) fileTransferInterruption {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fileTransferTimedOut
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return fileTransferCanceledByUser
	}
	return fileTransferNotInterrupted
}

func (s Runner) cancelFileTransferRecord(runtime *Runtime, transferID int64, message string) bool {
	return s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.store.Cancel(ctx, transferID, message)
	})
}

func (s Runner) persistFileTransferTerminal(runtime *Runtime, transferID int64, persist func(context.Context) (bool, error)) bool {
	return transferjobs.PersistTerminal(runtime.finalization.Context(), "file transfer", transferID, persist, func(ctx context.Context) (string, error) {
		item, err := runtime.store.Get(ctx, transferID)
		return item.Status, err
	})
}

func (s Runner) persistFileTransferBatchTerminal(runtime *Runtime, batchID int64, persist func(context.Context) (bool, error)) bool {
	return transferjobs.PersistTerminal(runtime.finalization.Context(), "file transfer batch", batchID, persist, func(ctx context.Context) (string, error) {
		item, err := runtime.store.GetBatch(ctx, batchID)
		return item.Status, err
	})
}

func fileTransferFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errFileTransferTimedOut) {
		return errFileTransferTimedOut.Error()
	}
	return fmt.Sprintf("file transfer failed: %v", err)
}

func credentialSafeErrorMessage(execution *Execution, prefix string, err error) string {
	if err == nil || execution == nil {
		return prefix
	}
	if execution.Boundary.Redact(err.Error()) != err.Error() {
		return prefix
	}
	message := connectorapi.PresentedErrorMessage(execution.Adapter, prefix, err)
	if execution.Boundary.Redact(message) != message {
		return prefix
	}
	return message
}
