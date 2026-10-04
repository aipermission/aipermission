package serviceboundary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSafeErrorWithholdsCredentialCausesAndKeepsCancellation(t *testing.T) {
	boundary := testBoundary(t)
	for _, form := range reflectedForms() {
		original := fmt.Errorf("untrusted response %s: %w", form, context.DeadlineExceeded)
		safe := boundary.SafeError(original)
		if !errors.Is(safe, ErrReflectedCredential) || !errors.Is(safe, context.DeadlineExceeded) || errors.Is(safe, original) || strings.Contains(safe.Error(), form) {
			t.Fatalf("unsafe reflected cause or lost timeout class: %v", safe)
		}
	}
	original := fmt.Errorf("ordinary network timeout: %w", context.DeadlineExceeded)
	if safe := boundary.SafeError(original); safe != original {
		t.Fatalf("ordinary transport cause changed: %v", safe)
	}
	if err := boundary.SafeError(nil); err != nil {
		t.Fatal(err)
	}
	if err := (*Boundary)(nil).SafeError(original); !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing boundary did not fail closed: %v", err)
	}
}

func TestReadErrorNeverRetainsUntrustedDiagnostics(t *testing.T) {
	for _, cause := range []error{errors.New("read failed"), context.Canceled, context.DeadlineExceeded, errors.Join(context.Canceled, context.DeadlineExceeded)} {
		original := fmt.Errorf("%s: %w", fixtureToken, cause)
		safe := ReadError(original)
		if safe == nil || strings.Contains(safe.Error(), fixtureToken) || errors.Is(safe, original) {
			t.Fatalf("untrusted reader diagnostic survived: %v", safe)
		}
		for _, standard := range []error{context.Canceled, context.DeadlineExceeded} {
			if errors.Is(safe, standard) != errors.Is(original, standard) {
				t.Fatalf("cancellation class changed: %v", safe)
			}
		}
	}
	if err := ReadError(nil); err != nil {
		t.Fatal(err)
	}
}
