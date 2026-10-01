package connectormanagement

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCompletedCredentialCleanupEvidenceCancellationStopsEveryBoundary(t *testing.T) {
	for _, where := range []string{"before factory", "in factory", "in reader", "in projection"} {
		t.Run(where, func(t *testing.T) {
			scope, target, profile := cleanupEvidenceFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			factories, reads, projections := 0, 0, 0
			factory := scope.EvidenceCapabilities
			scope.EvidenceCapabilities = func(kind string) (connectors.RuntimeCapabilityResolver, error) {
				factories++
				if where == "in factory" {
					cancel()
				}
				return factory(kind)
			}
			connector := cleanupEvidenceConnector{read: func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error) {
				reads++
				if where == "in reader" {
					cancel()
				}
				return &connectors.ActionResult{Status: connectors.ResultCompleted}, nil
			}}
			scope.Runtime.RedactResult = func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error) {
				projections++
				cancel()
				return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
			}
			if where == "before factory" {
				cancel()
			}
			outcome, handled, err := completedCredentialCleanupEvidence(ctx, scope, connector, target, profile)
			want := map[string][3]int{"before factory": {0, 0, 0}, "in factory": {1, 0, 0}, "in reader": {1, 1, 0}, "in projection": {1, 1, 1}}[where]
			if !handled || !errors.Is(err, context.Canceled) || outcome.Required || [3]int{factories, reads, projections} != want {
				t.Fatalf("canceled evidence advanced: %#v %t %v calls=%d/%d/%d", outcome, handled, err, factories, reads, projections)
			}
		})
	}
}

func TestCompletedCredentialCleanupEvidenceUnavailableBindingFailsClosed(t *testing.T) {
	for _, scenario := range []string{"nil context", "missing factory", "nil resolver", "typed nil resolver", "foreign profile", "foreign connector", "missing target", "missing profile", "missing projector"} {
		t.Run(scenario, func(t *testing.T) {
			scope, target, profile := cleanupEvidenceFixture(t)
			var ctx context.Context = t.Context()
			switch scenario {
			case "nil context":
				ctx = nil
			case "missing factory":
				scope.EvidenceCapabilities = nil
			case "nil resolver":
				scope.EvidenceCapabilities = func(string) (connectors.RuntimeCapabilityResolver, error) { return nil, nil }
			case "typed nil resolver":
				scope.EvidenceCapabilities = func(string) (connectors.RuntimeCapabilityResolver, error) {
					return (*cleanupEvidenceCapabilities)(nil), nil
				}
			case "foreign profile":
				profile.TargetID++
			case "foreign connector":
				profile.ConnectorKind = "other"
			case "missing target":
				target.ID = 0
			case "missing profile":
				profile.ID = 0
			case "missing projector":
				scope.Runtime.RedactResult = nil
			}
			connector := cleanupEvidenceConnector{read: func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error) {
				t.Fatal("unavailable evidence scope invoked connector reader")
				return nil, nil
			}}
			if outcome, handled, err := completedCredentialCleanupEvidence(ctx, scope, connector, target, profile); !handled || err == nil || outcome.Required {
				t.Fatalf("unavailable evidence scope accepted: %#v %t %v", outcome, handled, err)
			}
		})
	}
}

func TestCompletedCredentialCleanupEvidenceProjectionCannotChangeFinality(t *testing.T) {
	scope, target, profile := cleanupEvidenceFixture(t)
	scope.Runtime.RedactResult = func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error) {
		return connectors.ActionResult{Status: connectors.ResultRunning}, nil
	}
	connector := cleanupEvidenceConnector{read: func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error) {
		return &connectors.ActionResult{Status: connectors.ResultCompleted}, nil
	}}
	if outcome, handled, err := completedCredentialCleanupEvidence(t.Context(), scope, connector, target, profile); !handled || err == nil || outcome.Required {
		t.Fatalf("nonterminal projection accepted: %#v %t %v", outcome, handled, err)
	}
}
