package credentialresource

import (
	"errors"
	"fmt"
	"testing"
)

func TestResourceErrorIdentitySurvivesWrapping(t *testing.T) {
	for _, scenario := range []struct {
		cause error
		other error
		text  string
	}{
		{ErrCredentialResourceNotFound, ErrCredentialResourceNameExists, "connector credential resource not found"},
		{ErrCredentialResourceNameExists, ErrCredentialResourceNotFound, "connector credential resource name already exists"},
	} {
		wrapped := fmt.Errorf("resource operation: %w", scenario.cause)
		if scenario.cause.Error() != scenario.text || !errors.Is(wrapped, scenario.cause) || errors.Is(wrapped, scenario.other) ||
			errors.Is(wrapped, errors.New(scenario.text)) {
			t.Fatalf("resource error identity changed: %v", wrapped)
		}
	}
}
