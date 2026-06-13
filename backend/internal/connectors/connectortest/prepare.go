package connectortest

import (
	"context"
	"reflect"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type TestingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

func AssertPrepareActionDeterministic(t TestingT, connector connectors.Connector, request connectors.ActionRequest) {
	t.Helper()

	first, err := connector.PrepareAction(context.Background(), request)
	if err != nil {
		t.Fatalf("first PrepareAction failed: %v", err)
	}
	second, err := connector.PrepareAction(context.Background(), request)
	if err != nil {
		t.Fatalf("second PrepareAction failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("PrepareAction is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}
