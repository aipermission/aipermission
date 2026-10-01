package connectormanagement

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func credentialOperationRuntimeFixture() (connectortargets.Target, connectortargets.CredentialProfile) {
	return connectortargets.Target{ID: 4, ProjectID: 12, ConnectorKind: "fixture", Name: "target",
			Config: map[string]any{"endpoint": "current-endpoint"}, UpdatedAt: "current-target-revision"},
		connectortargets.CredentialProfile{ID: 7, TargetID: 4, ConnectorKind: "fixture", Kind: "password",
			Label: "selected", Public: map[string]any{"user": "operator"}, EncryptedSecretJSON: "encrypted-profile",
			UpdatedAt: "current-profile-revision"}
}

func TestPrepareCredentialOperationRuntimeRejectsInvalidBindingAndPorts(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(*connectortargets.Target, *connectortargets.CredentialProfile, *CredentialRuntimePorts)
	}{
		{"zero target id", func(t *connectortargets.Target, _ *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			t.ID = 0
		}},
		{"negative target id", func(t *connectortargets.Target, _ *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			t.ID = -1
		}},
		{"zero profile id", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.ID = 0
		}},
		{"negative profile id", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.ID = -1
		}},
		{"zero profile target", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.TargetID = 0
		}},
		{"negative profile target", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.TargetID = -1
		}},
		{"foreign profile", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.TargetID++
		}},
		{"empty target kind", func(t *connectortargets.Target, _ *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			t.ConnectorKind = ""
		}},
		{"empty profile kind", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.ConnectorKind = ""
		}},
		{"both kinds empty", func(t *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			t.ConnectorKind, p.ConnectorKind = "", ""
		}},
		{"foreign connector kind", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.ConnectorKind = "other"
		}},
		{"empty credential kind", func(_ *connectortargets.Target, p *connectortargets.CredentialProfile, _ *CredentialRuntimePorts) {
			p.Kind = ""
		}},
		{"missing decryptor", func(_ *connectortargets.Target, _ *connectortargets.CredentialProfile, p *CredentialRuntimePorts) {
			p.DecryptSecret = nil
		}},
		{"missing runtime factory", func(_ *connectortargets.Target, _ *connectortargets.CredentialProfile, p *CredentialRuntimePorts) {
			p.RuntimeContext = nil
		}},
		{"missing result projector", func(_ *connectortargets.Target, _ *connectortargets.CredentialProfile, p *CredentialRuntimePorts) {
			p.RedactResult = nil
		}},
		{"missing text redactor", func(_ *connectortargets.Target, _ *connectortargets.CredentialProfile, p *CredentialRuntimePorts) {
			p.RedactText = nil
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			target, profile := credentialOperationRuntimeFixture()
			ports := managementCredentialRuntimePorts()
			ports.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
				t.Fatal("invalid binding or ports reached decryption")
				return nil, nil
			}
			ports.RuntimeContext = func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, CredentialBoundary) connectors.RuntimeContext {
				t.Fatal("invalid binding or ports reached runtime factory")
				return connectors.RuntimeContext{}
			}
			scenario.mutate(&target, &profile, &ports)
			runtime, boundary, err := PrepareCredentialOperationRuntime(t.Context(), ports, target, profile)
			if err == nil || boundary.Valid() || !reflect.DeepEqual(runtime, connectors.RuntimeContext{}) {
				t.Fatalf("runtime=%#v boundary valid=%t error=%v", runtime, boundary.Valid(), err)
			}
		})
	}
}

func TestPrepareCredentialOperationRuntimeCancellationAndDecryptFailure(t *testing.T) {
	decryptFailure := errors.New("injected decryption failure")
	for _, scenario := range []struct {
		name         string
		want         error
		wantDecrypts int
	}{
		{"nil context", nil, 0},
		{"already canceled", context.Canceled, 0},
		{"deadline exceeded", context.DeadlineExceeded, 0},
		{"decryption failed", decryptFailure, 1},
		{"canceled during decryption", context.Canceled, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			target, profile := credentialOperationRuntimeFixture()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var input context.Context = ctx
			switch scenario.name {
			case "nil context":
				input = nil
			case "already canceled":
				cancel()
			case "deadline exceeded":
				expired, stop := context.WithDeadline(t.Context(), time.Unix(0, 0))
				defer stop()
				input = expired
			}
			decrypts := 0
			ports := managementCredentialRuntimePorts()
			ports.DecryptSecret = func(got context.Context, id int64, encrypted string) (map[string]any, error) {
				decrypts++
				if got != input || id != profile.ID || encrypted != profile.EncryptedSecretJSON {
					t.Fatal("decryptor lost selected profile or context")
				}
				if scenario.name == "decryption failed" {
					return map[string]any{"partial": "must-not-escape"}, decryptFailure
				}
				cancel()
				return map[string]any{"password": "must-not-escape"}, nil
			}
			ports.RuntimeContext = func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, CredentialBoundary) connectors.RuntimeContext {
				t.Fatal("failed or canceled preparation reached runtime factory")
				return connectors.RuntimeContext{}
			}
			runtime, boundary, err := PrepareCredentialOperationRuntime(input, ports, target, profile)
			if err == nil || (scenario.want != nil && !errors.Is(err, scenario.want)) || decrypts != scenario.wantDecrypts || boundary.Valid() || !reflect.DeepEqual(runtime, connectors.RuntimeContext{}) {
				t.Fatalf("runtime=%#v boundary valid=%t decrypts=%d error=%v", runtime, boundary.Valid(), decrypts, err)
			}
		})
	}
}

func TestPrepareCredentialOperationRuntimePreservesKnownAndDerivedBoundary(t *testing.T) {
	target, profile := credentialOperationRuntimeFixture()
	secrets := map[string]any{
		"password": "operation-password", "unused": "unread-secret",
		"nested": map[string]any{"tokens": []any{"nested-secret"}}, "number": int64(987654321),
	}
	ports := RuntimeCredentialPorts(CredentialStorage{}, nil,
		managementCredentialRuntimePorts().RedactResult, managementCredentialRuntimePorts().RedactText)
	decrypts, factories := 0, 0
	ports.DecryptSecret = func(ctx context.Context, id int64, encrypted string) (map[string]any, error) {
		decrypts++
		if ctx != t.Context() || id != profile.ID || encrypted != profile.EncryptedSecretJSON {
			t.Fatal("decryptor lost selected profile or context")
		}
		return secrets, nil
	}
	factory := ports.RuntimeContext
	ports.RuntimeContext = func(gotTarget connectortargets.Target, gotProfile connectortargets.CredentialProfile, gotSecrets map[string]any, boundary CredentialBoundary) connectors.RuntimeContext {
		factories++
		if !reflect.DeepEqual(gotTarget, target) || !reflect.DeepEqual(gotProfile, profile) || !reflect.DeepEqual(gotSecrets, secrets) || !boundary.Valid() {
			t.Fatal("runtime factory lost the bound snapshot, secrets or boundary")
		}
		for _, secret := range []string{"operation-password", "unread-secret", "nested-secret"} {
			if boundary.Redact(secret) == secret {
				t.Fatalf("factory received incomplete boundary for %q", secret)
			}
		}
		boundary.Add("factory-derived-secret")
		return factory(gotTarget, gotProfile, gotSecrets, boundary)
	}
	runtime, boundary, err := PrepareCredentialOperationRuntime(t.Context(), ports, target, profile)
	if err != nil || decrypts != 1 || factories != 1 || !boundary.Valid() {
		t.Fatalf("decrypts=%d factories=%d boundary valid=%t error=%v", decrypts, factories, boundary.Valid(), err)
	}
	if runtime.Target.Ref != connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID) || runtime.Target.ProjectID != target.ProjectID || runtime.Target.UpdatedAt != target.UpdatedAt || !reflect.DeepEqual(runtime.Profile, connectortargets.CredentialProfileView(profile)) {
		t.Fatalf("runtime lost authority fields: %#v", runtime)
	}
	for name, want := range map[string]string{"password": "operation-password", "number": "987654321"} {
		if got, err := runtime.Secrets.GetSecret(t.Context(), name); err != nil || got != want {
			t.Fatalf("secret %s=%q error=%v", name, got, err)
		}
	}
	registrar, ok := runtime.Secrets.(connectors.SensitiveValueRegistrar)
	if !ok {
		t.Fatal("runtime secrets lost sensitive-value registration")
	}
	registrar.RegisterSensitiveValue("adapter-derived-secret")
	for _, secret := range []string{"operation-password", "unread-secret", "nested-secret", "987654321", "factory-derived-secret", "adapter-derived-secret"} {
		if boundary.Redact("prefix "+secret+" suffix") == "prefix "+secret+" suffix" {
			t.Fatalf("returned boundary lost %q", secret)
		}
	}
	runtime.Target.Config["endpoint"] = "changed"
	runtime.Profile.Public["user"] = "changed"
	if target.Config["endpoint"] != "current-endpoint" || profile.Public["user"] != "operator" {
		t.Fatal("runtime metadata aliases the loaded snapshot")
	}
}

func TestPrepareCredentialOperationRuntimeWithoutEncryptedSecret(t *testing.T) {
	target, profile := credentialOperationRuntimeFixture()
	profile.EncryptedSecretJSON = ""
	ports := managementCredentialRuntimePorts()
	ports.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
		t.Fatal("empty encrypted secret reached decryptor")
		return nil, nil
	}
	factories := 0
	ports.RuntimeContext = func(gotTarget connectortargets.Target, gotProfile connectortargets.CredentialProfile, secrets map[string]any, boundary CredentialBoundary) connectors.RuntimeContext {
		factories++
		if !reflect.DeepEqual(gotTarget, target) || !reflect.DeepEqual(gotProfile, profile) || secrets == nil || len(secrets) != 0 || !boundary.Valid() || !boundary.Empty() {
			t.Fatal("secretless profile did not receive an initialized empty boundary and secret map")
		}
		return connectors.RuntimeContext{Target: connectors.TargetView{ID: target.ID}}
	}
	runtime, boundary, err := PrepareCredentialOperationRuntime(t.Context(), ports, target, profile)
	if err != nil || factories != 1 || runtime.Target.ID != target.ID || !boundary.Valid() || !boundary.Empty() {
		t.Fatalf("runtime=%#v factories=%d boundary valid=%t error=%v", runtime, factories, boundary.Valid(), err)
	}
}

func TestPrepareCredentialOperationRuntimeCanceledDuringFactory(t *testing.T) {
	target, profile := credentialOperationRuntimeFixture()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ports := managementCredentialRuntimePorts()
	decrypts, factories := 0, 0
	ports.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
		decrypts++
		return map[string]any{"password": "late-factory-secret"}, nil
	}
	ports.RuntimeContext = func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, CredentialBoundary) connectors.RuntimeContext {
		factories++
		cancel()
		return connectors.RuntimeContext{Target: connectors.TargetView{ID: target.ID}}
	}
	runtime, boundary, err := PrepareCredentialOperationRuntime(ctx, ports, target, profile)
	if !errors.Is(err, context.Canceled) || decrypts != 1 || factories != 1 || boundary.Valid() || !reflect.DeepEqual(runtime, connectors.RuntimeContext{}) {
		t.Fatalf("runtime=%#v boundary valid=%t decrypts=%d factories=%d error=%v", runtime, boundary.Valid(), decrypts, factories, err)
	}
}

func TestPrepareCredentialOperationRuntimeSecretAccessRejectsInvalidContext(t *testing.T) {
	target, profile := credentialOperationRuntimeFixture()
	ports := RuntimeCredentialPorts(CredentialStorage{}, nil,
		managementCredentialRuntimePorts().RedactResult, managementCredentialRuntimePorts().RedactText)
	ports.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
		return map[string]any{"password": "scoped-secret"}, nil
	}
	runtime, _, err := PrepareCredentialOperationRuntime(t.Context(), ports, target, profile)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		value, err := runtime.Secrets.GetSecret(ctx, "password")
		if err == nil || value != "" || (ctx != nil && !errors.Is(err, context.Canceled)) {
			t.Fatalf("invalid-context secret access returned value=%q error=%v", value, err)
		}
	}
}
