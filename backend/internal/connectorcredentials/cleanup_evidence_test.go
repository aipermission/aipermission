package connectorcredentials

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type cleanupEvidenceConnector struct {
	connectors.Connector
	read func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error)
}

func (connector cleanupEvidenceConnector) ReadCompletedCredentialCleanup(ctx context.Context, evidence connectors.CleanupEvidenceContext, profile connectors.CredentialProfileView) (*connectors.ActionResult, error) {
	return connector.read(ctx, evidence, profile)
}

func (cleanupEvidenceConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	panic("cleanup evidence must not execute remote actions")
}

func (cleanupEvidenceConnector) CleanupProvisionedCredentialProfile(context.Context, connectors.RuntimeContext, connectors.CredentialProfileView) (connectors.ActionResult, error) {
	panic("cleanup evidence must not dispatch authenticated cleanup")
}

type cleanupFixture struct {
	ctx          context.Context
	ports        RuntimePorts
	target       connectortargets.Target
	profile      connectortargets.CredentialProfile
	connector    connectors.Connector
	factory      func(string) (connectors.RuntimeCapabilityResolver, error)
	capabilities func() (connectors.RuntimeCapabilityResolver, error)
	read         func() (*connectors.ActionResult, error)
	project      func(connectors.ActionResult, actionresult.CredentialBoundary) (connectors.ActionResult, error)
	calls        [4]int // Capability factory, evidence reader, result projector, text redactor.
}

func newCleanupFixture(t *testing.T) *cleanupFixture {
	t.Helper()
	f := &cleanupFixture{
		ctx: t.Context(),
		target: connectortargets.Target{ID: 4, ProjectID: 12, ConnectorKind: "fixture", Name: "target",
			UpdatedAt: "current-target", Config: map[string]any{"endpoint": "current-endpoint"}},
		profile: connectortargets.CredentialProfile{ID: 7, TargetID: 4, ConnectorKind: "fixture", Kind: "password",
			Label: "selected", UpdatedAt: "current-profile", Public: map[string]any{"marker": "generated"}, EncryptedSecretJSON: "unread-ciphertext"},
	}
	f.capabilities = func() (connectors.RuntimeCapabilityResolver, error) { return testCapabilities{}, nil }
	f.read = func() (*connectors.ActionResult, error) {
		return &connectors.ActionResult{Status: connectors.ResultCompleted, Output: "unprojected"}, nil
	}
	f.project = func(result connectors.ActionResult, _ actionresult.CredentialBoundary) (connectors.ActionResult, error) {
		result.Output = "projected"
		return result, nil
	}
	f.factory = func(kind string) (connectors.RuntimeCapabilityResolver, error) {
		f.calls[0]++
		if kind != f.target.ConnectorKind {
			t.Fatalf("factory kind=%q", kind)
		}
		return f.capabilities()
	}
	f.connector = cleanupEvidenceConnector{read: func(ctx context.Context, evidence connectors.CleanupEvidenceContext, profile connectors.CredentialProfileView) (*connectors.ActionResult, error) {
		f.calls[1]++
		if ctx != f.ctx || ctx == nil || ctx.Err() != nil || !reflect.DeepEqual(evidence.Target, TargetView(f.target, f.profile.ID)) ||
			!reflect.DeepEqual(profile, connectortargets.CredentialProfileView(f.profile)) || evidence.Capabilities != (testCapabilities{}) {
			t.Fatal("reader lost current public identity or received unexpected capability authority")
		}
		evidence.Target.Config["endpoint"] = "reader-mutation"
		profile.Public["marker"] = "reader-mutation"
		return f.read()
	}}
	f.ports = RuntimePorts{
		DecryptSecret: func(context.Context, int64, string) (map[string]any, error) {
			t.Fatal("cleanup evidence/projection must not decrypt")
			return nil, nil
		},
		RuntimeContext: func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, actionresult.CredentialBoundary) connectors.RuntimeContext {
			t.Fatal("cleanup evidence/projection must not construct authenticated runtime")
			return connectors.RuntimeContext{}
		},
		RedactResult: func(ctx context.Context, result connectors.ActionResult, boundary actionresult.CredentialBoundary) (connectors.ActionResult, error) {
			f.calls[2]++
			if ctx != f.ctx || ctx == nil || ctx.Err() != nil || !boundary.Valid() {
				t.Fatal("projector received unavailable context/boundary")
			}
			return f.project(result, boundary)
		},
		RedactText: func(ctx context.Context, text string) string {
			f.calls[3]++
			if ctx != f.ctx {
				t.Fatal("text redaction lost caller context")
			}
			return strings.ReplaceAll(text, "private-diagnostic", "safe-diagnostic")
		},
	}
	return f
}

func (f *cleanupFixture) run(ctx context.Context) (*connectors.ActionResult, bool, error) {
	f.ctx = ctx
	return f.ports.ReadCompletedCleanupEvidence(ctx, f.factory, f.connector, f.target, f.profile)
}

func TestReadCompletedCleanupEvidenceProjectsIsolatedPublicAuthority(t *testing.T) {
	f := newCleanupFixture(t)
	f.project = func(result connectors.ActionResult, boundary actionresult.CredentialBoundary) (connectors.ActionResult, error) {
		if result.Output != "unprojected" || boundary.Redact("unread-ciphertext") != "unread-ciphertext" {
			t.Fatal("projection lost result or treated encrypted profile as delivered credentials")
		}
		result.Output, result.DisplayText = "projected", "confirmed"
		return result, nil
	}
	got, handled, err := f.run(t.Context())
	want := &connectors.ActionResult{Status: connectors.ResultCompleted, Output: "projected", DisplayText: "confirmed"}
	if err != nil || !handled || !reflect.DeepEqual(got, want) || f.calls != [4]int{1, 1, 1, 0} ||
		f.target.Config["endpoint"] != "current-endpoint" || f.profile.Public["marker"] != "generated" {
		t.Fatalf("evidence result=%#v handled=%v err=%v calls=%v", got, handled, err, f.calls)
	}
}

func TestReadCompletedCleanupEvidenceFallsBackOnlyWithoutEvidence(t *testing.T) {
	for _, mode := range []string{"nil connector", "no contract", "nil result"} {
		t.Run(mode, func(t *testing.T) {
			f := newCleanupFixture(t)
			want := [4]int{1, 1, 0, 0}
			if mode != "nil result" {
				f.connector, f.factory, f.ports = nil, nil, RuntimePorts{}
				if mode == "no contract" {
					f.connector = struct{ connectors.Connector }{}
				}
				want = [4]int{}
			} else {
				f.read = func() (*connectors.ActionResult, error) { return nil, nil }
			}
			got, handled, err := f.run(t.Context())
			if got != nil || handled || err != nil || f.calls != want {
				t.Fatalf("normal cleanup fallback=%#v %v %v calls=%v", got, handled, err, f.calls)
			}
		})
	}
}

func TestReadCompletedCleanupEvidenceRejectsUnavailableIdentityAndDependencies(t *testing.T) {
	for _, mode := range []string{"nil context", "factory", "decrypt", "runtime", "projector", "redactor", "target", "profile", "foreign target", "missing kind", "foreign kind", "nil resolver", "typed nil resolver"} {
		t.Run(mode, func(t *testing.T) {
			f := newCleanupFixture(t)
			var ctx context.Context = t.Context()
			want := [4]int{}
			switch mode {
			case "nil context":
				ctx = nil
			case "factory":
				f.factory = nil
			case "decrypt":
				f.ports.DecryptSecret = nil
			case "runtime":
				f.ports.RuntimeContext = nil
			case "projector":
				f.ports.RedactResult = nil
			case "redactor":
				f.ports.RedactText = nil
			case "target":
				f.target.ID = 0
			case "profile":
				f.profile.ID = 0
			case "foreign target":
				f.profile.TargetID++
			case "missing kind":
				f.target.ConnectorKind, f.profile.ConnectorKind = "", ""
			case "foreign kind":
				f.profile.ConnectorKind = "other"
			case "nil resolver", "typed nil resolver":
				want[0] = 1
				f.capabilities = func() (connectors.RuntimeCapabilityResolver, error) {
					if mode == "nil resolver" {
						return nil, nil
					}
					return (*testCapabilities)(nil), nil
				}
			}
			got, handled, err := f.run(ctx)
			if got != nil || !handled || err == nil || err.Error() != "connector cleanup evidence runtime is unavailable" || f.calls != want {
				t.Fatalf("unavailable evidence advanced: %#v %v %v calls=%v want=%v", got, handled, err, f.calls, want)
			}
		})
	}
}

func TestReadCompletedCleanupEvidenceStopsAtEveryCancellationBoundary(t *testing.T) {
	for _, mode := range []string{"before factory", "after factory", "after nil factory", "after reader", "after nil reader", "after failed reader", "after projection"} {
		t.Run(mode, func(t *testing.T) {
			f := newCleanupFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := [4]int{}
			switch mode {
			case "before factory":
				cancel()
			case "after factory", "after nil factory":
				want[0] = 1
				f.capabilities = func() (connectors.RuntimeCapabilityResolver, error) {
					cancel()
					if mode == "after nil factory" {
						return nil, nil
					}
					return testCapabilities{}, nil
				}
			case "after reader", "after nil reader", "after failed reader":
				want = [4]int{1, 1, 0, 0}
				f.read = func() (*connectors.ActionResult, error) {
					cancel()
					if mode == "after nil reader" {
						return nil, nil
					}
					if mode == "after failed reader" {
						return nil, errors.New("private-diagnostic")
					}
					return &connectors.ActionResult{Status: connectors.ResultCompleted}, nil
				}
			case "after projection":
				want = [4]int{1, 1, 1, 0}
				f.project = func(result connectors.ActionResult, _ actionresult.CredentialBoundary) (connectors.ActionResult, error) {
					cancel()
					return result, nil
				}
			}
			got, handled, err := f.run(ctx)
			if got != nil || !handled || !errors.Is(err, context.Canceled) || f.calls != want {
				t.Fatalf("canceled evidence advanced: %#v %v %v calls=%v want=%v", got, handled, err, f.calls, want)
			}
		})
	}
}

func TestReadCompletedCleanupEvidenceRedactsFailuresWithoutFallback(t *testing.T) {
	for _, mode := range []string{"factory", "reader", "projector", "input status", "projected status"} {
		t.Run(mode, func(t *testing.T) {
			f := newCleanupFixture(t)
			failure := errors.New("private-diagnostic")
			want := [4]int{1, 1, 1, 1}
			message := "safe-diagnostic"
			switch mode {
			case "factory":
				want = [4]int{1, 0, 0, 1}
				f.capabilities = func() (connectors.RuntimeCapabilityResolver, error) { return testCapabilities{}, failure }
			case "reader":
				want = [4]int{1, 1, 0, 1}
				f.read = func() (*connectors.ActionResult, error) {
					return &connectors.ActionResult{Status: connectors.ResultCompleted}, failure
				}
			case "projector":
				message = "process credential cleanup result: safe-diagnostic"
				f.project = func(result connectors.ActionResult, _ actionresult.CredentialBoundary) (connectors.ActionResult, error) {
					return result, failure
				}
			case "input status":
				want = [4]int{1, 1, 0, 1}
				message = "credential cleanup returned status \"safe-diagnostic\""
				f.read = func() (*connectors.ActionResult, error) {
					return &connectors.ActionResult{Status: "private-diagnostic"}, nil
				}
			case "projected status":
				message = "credential cleanup returned status \"safe-diagnostic\""
				f.project = func(result connectors.ActionResult, _ actionresult.CredentialBoundary) (connectors.ActionResult, error) {
					result.Status = "private-diagnostic"
					return result, nil
				}
			}
			got, handled, err := f.run(t.Context())
			if got != nil || !handled || err == nil || err.Error() != message || f.calls != want {
				t.Fatalf("failed evidence accepted/leaked: %#v %v %v calls=%v want=%v", got, handled, err, f.calls, want)
			}
		})
	}
}
