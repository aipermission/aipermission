package transfer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClassifyFileTransferInterruption(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want fileTransferInterruption
	}{
		{name: "deadline context", ctx: deadlineCtx, err: context.Canceled, want: fileTransferTimedOut},
		{name: "deadline error", ctx: context.Background(), err: context.DeadlineExceeded, want: fileTransferTimedOut},
		{name: "user cancellation", ctx: canceledCtx, err: context.Canceled, want: fileTransferCanceledByUser},
		{name: "connector cancellation without local cancel", ctx: context.Background(), err: context.Canceled, want: fileTransferNotInterrupted},
		{name: "ordinary failure", ctx: context.Background(), err: errors.New("network failed"), want: fileTransferNotInterrupted},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classifyFileTransferInterruption(testCase.ctx, testCase.err); got != testCase.want {
				t.Fatalf("classification=%d want=%d", got, testCase.want)
			}
		})
	}
}

func TestFileTransferFailureMessageKeepsTimeoutExplicit(t *testing.T) {
	if got := fileTransferFailureMessage(errFileTransferTimedOut); got != "file transfer timed out" {
		t.Fatalf("timeout message=%q", got)
	}
}
