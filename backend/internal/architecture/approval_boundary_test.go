package architecture

import (
	"reflect"
	"sort"
	"testing"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func TestConnectorApprovalBoundaryExposesOnlyRequestReads(t *testing.T) {
	scope := reflect.TypeOf(connectormgmt.ConnectorApprovalScope{})
	if _, ok := scope.FieldByName("Database"); ok {
		t.Fatal("connector approval scope exposes a mutable database handle")
	}
	requests, ok := scope.FieldByName("Requests")
	if !ok || requests.Type.Kind() != reflect.Interface {
		t.Fatal("connector approval scope must expose an opaque request reader")
	}

	methods := make([]string, 0, requests.Type.NumMethod())
	for index := 0; index < requests.Type.NumMethod(); index++ {
		methods = append(methods, requests.Type.Method(index).Name)
	}
	sort.Strings(methods)
	want := []string{"GetActionRequest", "ListActionRequests"}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("connector approval request authority = %v, want %v", methods, want)
	}
}
