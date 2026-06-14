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

func AssertActionListStable(t TestingT, connector connectors.Connector, target connectors.TargetView, profile connectors.CredentialProfileView) {
	t.Helper()

	baselineTarget := connectors.TargetView{ConnectorKind: connector.Kind()}
	baselineProfile := connectors.CredentialProfileView{ConnectorKind: connector.Kind(), Kind: profile.Kind}
	first, err := connector.GetActionList(context.Background(), baselineTarget, baselineProfile)
	if err != nil {
		t.Fatalf("baseline GetActionList failed: %v", err)
	}
	second, err := connector.GetActionList(context.Background(), target, profile)
	if err != nil {
		t.Fatalf("target GetActionList failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("GetActionList must be stable for the connector kind:\nfirst:  %#v\nsecond: %#v", first, second)
	}
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
