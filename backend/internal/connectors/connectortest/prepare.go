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
	if !actionsEqual(first, second) {
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

func actionsEqual(left []connectors.ActionDefinition, right []connectors.ActionDefinition) bool {
	if len(left) != len(right) {
		return false
	}
	leftByName := make(map[string]connectors.ActionDefinition, len(left))
	for _, action := range left {
		leftByName[action.Name] = action
	}
	for _, action := range right {
		if !actionEqual(leftByName[action.Name], action) {
			return false
		}
	}
	return true
}

func actionEqual(left connectors.ActionDefinition, right connectors.ActionDefinition) bool {
	if left.Name != right.Name ||
		left.Label != right.Label ||
		left.Description != right.Description ||
		left.Category != right.Category ||
		left.Risk != right.Risk ||
		left.OutputHint.Format != right.OutputHint.Format ||
		left.OutputHint.MaxRows != right.OutputHint.MaxRows ||
		left.OutputHint.MaxBytes != right.OutputHint.MaxBytes {
		return false
	}
	if len(left.OutputHint.SensitiveFields) != len(right.OutputHint.SensitiveFields) {
		return false
	}
	for index := range left.OutputHint.SensitiveFields {
		if left.OutputHint.SensitiveFields[index] != right.OutputHint.SensitiveFields[index] {
			return false
		}
	}
	return schemasEqual(left.InputSchema, right.InputSchema)
}

func schemasEqual(left connectors.Schema, right connectors.Schema) bool {
	if len(left.Fields) != len(right.Fields) {
		return false
	}
	for index := range left.Fields {
		if !reflect.DeepEqual(left.Fields[index], right.Fields[index]) {
			return false
		}
	}
	return true
}
