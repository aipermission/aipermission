package connectormanagement

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

func TestProjectManagementResponseAppliesEveryRedactionBoundary(t *testing.T) {
	const storedSecret = "stored-credential-secret"
	const connectorSecret = "connector-private-material"
	const customSecret = "CUSTOM-WORKSPACE-CANARY"
	const basicToken = "ghp_abcdefghijklmnopqrstuvwxyz123456"
	encodedSecret := base64.StdEncoding.EncodeToString([]byte(storedSecret))
	redactor, err := actionresult.NewRedactor(
		func(_ context.Context, value string) string {
			return strings.ReplaceAll(securitypolicy.RedactBasic(value), customSecret, "[CUSTOM]")
		},
		func(_ context.Context, value string) string { return securitypolicy.RedactBasic(value) },
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := CredentialRuntimePorts{
		RedactResult: func(ctx context.Context, result connectors.ActionResult, boundary CredentialBoundary) (connectors.ActionResult, error) {
			return redactor.ResultWithCredentialBoundary(ctx, result, boundary)
		},
	}
	status, payload, err := ProjectManagementResponse(t.Context(), runtime, connectors.ManagementResponse{
		StatusCode: 200,
		Payload: map[string]any{
			"stored": storedSecret, "encoded": encodedSecret, "connector": connectorSecret,
			"custom": customSecret, "basic": "token=" + basicToken,
		},
		SensitiveValues: []string{connectorSecret},
	}, actionresult.NewCredentialBoundary(map[string]any{"password": storedSecret}))
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	output := fmt.Sprint(payload)
	for _, secret := range []string{storedSecret, encodedSecret, connectorSecret, customSecret, basicToken} {
		if strings.Contains(output, secret) {
			t.Fatalf("projected management response contains %q: %s", secret, output)
		}
	}
}

func TestProjectManagementResponseFailsClosed(t *testing.T) {
	runtime := CredentialRuntimePorts{RedactResult: func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error) {
		return connectors.ActionResult{}, errors.New("projection failed")
	}}
	for _, response := range []connectors.ManagementResponse{
		{StatusCode: 199, Payload: map[string]any{"value": "x"}},
		{StatusCode: 200, Payload: map[string]any{"value": "x"}},
	} {
		if _, _, err := ProjectManagementResponse(t.Context(), runtime, response, CredentialBoundary{}); err == nil {
			t.Fatalf("response %#v did not fail closed", response)
		}
	}
}

func TestManagementResponseNeverSerializesBoundaryValues(t *testing.T) {
	encoded, err := json.Marshal(connectors.ManagementResponse{
		StatusCode: 200, Payload: map[string]any{"ok": true}, SensitiveValues: []string{"private-material"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-material") {
		t.Fatalf("management response serialized credential boundary: %s", encoded)
	}
}
