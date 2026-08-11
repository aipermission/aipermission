package conformance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const conformanceEnv = "AIPERMISSION_CONFORMANCE"

type fixtureSecrets map[string]string

func (s fixtureSecrets) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := s[name]
	if !ok {
		return "", fmt.Errorf("conformance secret %q is not configured", name)
	}
	return value, nil
}

type fixtureCapabilities struct{}

func (fixtureCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return directNetworkTransport{}
	}
	return nil
}

type directNetworkTransport struct{}

func (directNetworkTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (directNetworkTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	address := net.JoinHostPort(request.Host, strconv.Itoa(request.Port))
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
}

func requireConformance(t *testing.T) {
	t.Helper()
	if os.Getenv(conformanceEnv) != "1" {
		t.Skip("real-service connector conformance is disabled; run `make connector-conformance`")
	}
}

func fixtureHost(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func fixturePort(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("%s must be a valid TCP port, got %q", name, value)
	}
	return port
}

func assertConnection(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	testable, ok := connector.(connectors.TestableConnector)
	if !ok {
		t.Fatalf("connector %q does not implement TestableConnector", connector.Kind())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	result, err := testable.TestConnection(ctx, runtime)
	if err != nil {
		t.Fatalf("test %s connection: %v", connector.Kind(), err)
	}
	if result.Status != connectors.TestOK {
		t.Fatalf("%s connection status = %q: %s (%#v)", connector.Kind(), result.Status, result.Message, result.Details)
	}
}

func executeAction(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext, actionName string, input map[string]any) connectors.ActionResult {
	t.Helper()
	actions, err := connector.GetActionList(t.Context(), runtime.Target, runtime.Profile)
	if err != nil {
		t.Fatalf("list %s actions: %v", connector.Kind(), err)
	}
	var definition *connectors.ActionDefinition
	for index := range actions {
		if actions[index].Name == actionName {
			definition = &actions[index]
			break
		}
	}
	if definition == nil {
		t.Fatalf("connector %q does not advertise action %q", connector.Kind(), actionName)
	}
	prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{
		Source:     "conformance",
		Target:     runtime.Target,
		Profile:    runtime.Profile,
		ActionName: actionName,
		Input:      input,
		Reason:     "real-service connector conformance",
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("prepare %s.%s: %v", connector.Kind(), actionName, err)
	}
	if prepared.ConnectorKind != connector.Kind() || prepared.ActionName != actionName || prepared.Risk != definition.Risk {
		t.Fatalf("prepared action does not match advertised contract: %#v / %#v", prepared, *definition)
	}
	assertPreparedActionExcludesSecrets(t, prepared, runtime.Secrets)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	result, err := connector.ExecuteAction(ctx, runtime, prepared)
	if err != nil {
		t.Fatalf("execute %s.%s: %v", connector.Kind(), actionName, err)
	}
	if result.Status != connectors.ResultCompleted {
		t.Fatalf("%s.%s status = %q: %s (%#v)", connector.Kind(), actionName, result.Status, result.Error, result.Output)
	}
	return result
}

func assertPreparedActionExcludesSecrets(t *testing.T, prepared connectors.PreparedAction, accessor connectors.SecretAccessor) {
	t.Helper()
	secrets, ok := accessor.(fixtureSecrets)
	if !ok {
		t.Fatalf("conformance runtime uses unsupported secret accessor %T", accessor)
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		t.Fatalf("marshal prepared action: %v", err)
	}
	for name, value := range secrets {
		if value != "" && strings.Contains(string(payload), value) {
			t.Fatalf("prepared action exposed credential secret %q", name)
		}
	}
}

func assertResultContains(t *testing.T, result connectors.ActionResult, marker string) {
	t.Helper()
	payload, err := json.Marshal(result.Output)
	if err != nil {
		t.Fatalf("marshal action output: %v", err)
	}
	if !strings.Contains(string(payload), marker) {
		t.Fatalf("action output does not contain %q: %s", marker, payload)
	}
}
