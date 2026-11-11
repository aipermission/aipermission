package transferruntime

import (
	"context"
	"errors"
	"fmt"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"time"
)

func runBoundUpload(ctx context.Context, execution Execution, item filetransfer.Record, overwrite bool, options connectors.TransferOptions) (connectors.TransferResult, error) {
	worker := ctx
	if item.Source == filetransfer.SourceMCP {
		expires, err := time.Parse(time.RFC3339Nano, item.TempExpiresAt)
		if err != nil || !time.Now().Before(expires) || item.BatchID != 0 || execution.AuthorizeUpload == nil {
			return connectors.TransferResult{}, ErrExecutionStale
		}
		authorize := bindUploadAuthorization(worker, execution.AuthorizeUpload)
		guarded, stop, err := monitorUploadAuthorization(ctx, authorize)
		if err != nil {
			return connectors.TransferResult{}, err
		}
		defer stop()
		ctx = guarded
		options.AuthorizePublish = authorize
	}
	bindUploadContent(&options, item)
	result, err := execution.Adapter.UploadFile(ctx, execution.Gateway, execution.Runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, options)
	if err != nil && worker.Err() == nil && context.Cause(ctx) != nil && connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		err = errors.Join(context.Cause(ctx), err)
	}
	return result, err
}

func monitorUploadAuthorization(ctx context.Context, authorize connectors.FileUploadAuthorization) (context.Context, func(), error) {
	guarded, cancel := context.WithCancelCause(ctx)
	if err := checkUploadAuthorization(guarded, authorize); err != nil {
		cancel(err)
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, uploadAuthorizationError(err)
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
				if err := checkUploadAuthorization(guarded, authorize); err != nil {
					cancel(uploadAuthorizationError(err))
					return
				}
			}
		}
	}()
	return guarded, func() { cancel(context.Canceled); <-done }, nil
}

func uploadAuthorizationError(err error) error {
	if connectors.ErrorCode(err) == "upload_authorization_failed" {
		return err
	}
	return connectors.ClassifyActionError("upload_authorization_failed", "", map[string]any{
		"dispatch_stage": "upload_authorization", "retry_safe": false,
	}, fmt.Errorf("upload authorization check failed: %w", err))
}

func bindUploadAuthorization(owner context.Context, authorize connectors.FileUploadAuthorization) connectors.FileUploadAuthorization {
	return func(ctx context.Context) (context.Context, func(), error) {
		authorized, release, err := authorize(ctx)
		if err != nil && owner.Err() == nil {
			err = uploadAuthorizationError(err)
		}
		return authorized, release, err
	}
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
