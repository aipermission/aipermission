package connectorcredentials

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestSecretsRejectUnavailableContextBeforeLookupAndRegistration(t *testing.T) {
	for _, mode := range []string{"nil", "canceled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			switch mode {
			case "nil":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "expired":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				defer cancel()
			}
			boundary := actionresult.NewCredentialBoundary(nil)
			value, err := Secrets(map[string]any{"password": "unread-private-value"}, boundary).GetSecret(ctx, "password")
			if value != "" || err == nil || boundary.Redact("unread-private-value") != "unread-private-value" {
				t.Fatal("unavailable context exposed or registered secret")
			}
			if ctx != nil && !errors.Is(err, ctx.Err()) {
				t.Fatalf("lost cancellation cause: %v", err)
			}
		})
	}
}

func TestSecretsPreserveValuesAndSharedSensitiveBoundary(t *testing.T) {
	for _, test := range []struct {
		value any
		text  string
	}{{"private-password", "private-password"}, {123456, "123456"}, {false, "false"}} {
		boundary := actionresult.NewCredentialBoundary(nil)
		accessor := Secrets(map[string]any{"value": test.value, "nil": nil}, boundary)
		got, err := accessor.GetSecret(t.Context(), "value")
		if err != nil || got != test.text || boundary.Redact(test.text) == test.text {
			t.Fatalf("secret not projected: got=%q err=%v", got, err)
		}
		accessor.(connectors.SensitiveValueRegistrar).RegisterSensitiveValue("derived-private-value")
		if boundary.Redact("derived-private-value") == "derived-private-value" {
			t.Fatal("derived value registration lost shared boundary")
		}
		for _, name := range []string{"missing", "nil"} {
			if got, err := accessor.GetSecret(t.Context(), name); got != "" || !errors.Is(err, connectors.ErrSecretNotFound) {
				t.Fatalf("missing secret contract changed: got=%q err=%v", got, err)
			}
		}
	}
}

type testCapabilities struct{}

func (testCapabilities) RuntimeCapability(string) connectors.RuntimeCapability { return nil }

func TestContextPreservesCurrentAuthorityAndIsolatesMetadataKeys(t *testing.T) {
	target := connectortargets.Target{ID: 4, ProjectID: 12, ConnectorKind: "fixture", Name: "target",
		UpdatedAt: "current-target", Config: map[string]any{"database": "main"}}
	profile := connectortargets.CredentialProfile{ID: 7, TargetID: 4, ConnectorKind: "fixture", Kind: "password",
		Label: "selected", UpdatedAt: "current-profile", Public: map[string]any{"username": "admin"}, EncryptedSecretJSON: "ciphertext"}
	boundary := actionresult.NewCredentialBoundary(map[string]any{"password": "current-private-value"})
	runtime := Context(target, profile, map[string]any{"password": "current-private-value"}, boundary, testCapabilities{})
	if runtime.Target.ID != 4 || runtime.Target.ProjectID != 12 || runtime.Target.UpdatedAt != target.UpdatedAt ||
		runtime.Target.Ref != "fixture:4:7" || !reflect.DeepEqual(runtime.Profile, connectortargets.CredentialProfileView(profile)) ||
		runtime.Capabilities != (testCapabilities{}) || runtime.Principal != (connectors.Principal{}) {
		t.Fatalf("credential context lost authority or invented principal: %#v", runtime)
	}
	runtime.Target.Config["database"], runtime.Profile.Public["username"] = "changed", "changed"
	if target.Config["database"] != "main" || profile.Public["username"] != "admin" {
		t.Fatal("credential context aliases mutable metadata keys")
	}
	if value, err := runtime.Secrets.GetSecret(t.Context(), "password"); err != nil || value != "current-private-value" {
		t.Fatalf("context secret accessor=%q %v", value, err)
	}
	if err := runtime.Events.Emit(t.Context(), connectors.ActionEvent{}); err != nil {
		t.Fatal(err)
	}
	if metadata := CloneMetadata(nil); metadata == nil || len(metadata) != 0 {
		t.Fatal("nil metadata must remain an empty object")
	}
}
