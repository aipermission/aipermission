package transferruntime

import (
	"context"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"time"
)

func runBoundUpload(ctx context.Context, execution Execution, item filetransfer.Record, overwrite bool, options connectors.TransferOptions) (connectors.TransferResult, error) {
	if item.Source == filetransfer.SourceMCP {
		expires, err := time.Parse(time.RFC3339Nano, item.TempExpiresAt)
		if err != nil || !time.Now().Before(expires) || item.BatchID != 0 || execution.AuthorizeUpload == nil {
			return connectors.TransferResult{}, ErrExecutionStale
		}
		guarded, stop, err := monitorUploadAuthorization(ctx, execution.AuthorizeUpload)
		if err != nil {
			return connectors.TransferResult{}, err
		}
		defer stop()
		ctx = guarded
		options.AuthorizePublish = execution.AuthorizeUpload
	}
	bindUploadContent(&options, item)
	return execution.Adapter.UploadFile(ctx, execution.Gateway, execution.Runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, options)
}

func monitorUploadAuthorization(ctx context.Context, authorize connectors.FileUploadAuthorization) (context.Context, func(), error) {
	guarded, cancel := context.WithCancel(ctx)
	if err := checkUploadAuthorization(guarded, authorize); err != nil {
		cancel()
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-guarded.Done():
				return
			case <-ticker.C:
				if checkUploadAuthorization(guarded, authorize) != nil {
					cancel()
					return
				}
			}
		}
	}()
	return guarded, func() { cancel(); <-done }, nil
}

func checkUploadAuthorization(ctx context.Context, authorize connectors.FileUploadAuthorization) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	authorized, release, err := authorize(ctx)
	if err != nil {
		return err
	}
	if authorized == nil || release == nil {
		return ErrExecutionStale
	}
	defer release()
	return authorized.Err()
}

func uploadActor(execution Execution) (string, *int64) {
	if execution.UploadTokenID > 0 {
		return "mcp", &execution.UploadTokenID
	}
	return "user", nil
}

func bindUploadContent(options *connectors.TransferOptions, item filetransfer.Record) {
	if item.Source == filetransfer.SourceMCP && item.Direction == filetransfer.DirectionUpload {
		options.ExpectedContent = &connectors.FileContentIdentity{SizeBytes: item.SizeBytes, ChecksumSHA256: item.ChecksumSHA256}
	}
}
