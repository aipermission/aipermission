package apiadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type failingActionRequestFinisher struct {
	err         error
	hadDeadline bool
}

func (f *failingActionRequestFinisher) ConnectorFinishActionRequest(ctx context.Context, _ int64, _ connectors.ResultStatus, _ any, _ string, _ string, _ ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	_, f.hadDeadline = ctx.Deadline()
	return connectortargets.ActionRequest{}, f.err
}

func TestFinishRunningActionRequestPropagatesPersistenceFailure(t *testing.T) {
	want := errors.New("database unavailable")
	finisher := &failingActionRequestFinisher{err: want}
	err := finishRunningActionRequest(finisher, nil, 42, connectors.ResultError, nil, "", "failed", connectors.OutputHint{})
	if !errors.Is(err, want) {
		t.Fatalf("finish error = %v, want %v", err, want)
	}
	if !finisher.hadDeadline {
		t.Fatal("finish request did not receive a deadline")
	}
}
