package transport

import (
	"encoding/json"
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestConsoleLaunchOverrideRequiresConnectorOwnedDelegation(t *testing.T) {
	for _, nested := range []bool{false, true} {
		request := connectorapi.LiveConsoleOpenRequest{
			Generation: 7, HasEnvironment: true, NestedTransport: nested,
			Params: map[string]any{"force_shell_command": " trusted-wrapper ", "NestedTransport": true},
		}
		options := liveConsoleOptions(request)
		want := ""
		if nested {
			want = "trusted-wrapper"
		}
		if options.ForceShellCommand != want || options.Generation != 7 || !options.HasEnvironment {
			t.Fatalf("nested=%v launch options=%#v; want command %q", nested, options, want)
		}
	}
}

func TestConsoleLaunchProvenanceCannotBeDeserialized(t *testing.T) {
	var request connectorapi.LiveConsoleOpenRequest
	if err := json.Unmarshal([]byte(`{"NestedTransport":true,"nested_transport":true,"Params":{"force_shell_command":"untrusted-command"}}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.NestedTransport || liveConsoleOptions(request).ForceShellCommand != "" {
		t.Fatal("serialized request elevated console launch provenance")
	}
}
