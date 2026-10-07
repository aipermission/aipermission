package connectortargets

import (
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestCapacityErrorPreservesIdentityAndExplainsStoredLimits(t *testing.T) {
	limits := actioncapacity.DefaultLimits()
	for _, test := range []struct {
		name  string
		usage actioncapacity.Usage
	}{
		{"stored_rows", actioncapacity.Usage{Rows: limits.Rows + 1}},
		{"stored_bytes", actioncapacity.Usage{Bytes: limits.Bytes + 1}},
		{"running_requests", actioncapacity.Usage{Running: limits.Running + 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := &ActionRequestCapacityError{Usage: test.usage, Limits: limits}
			if !errors.Is(err, ErrActionRequestCapacity) || !strings.Contains(err.Error(), "limit="+test.name) {
				t.Fatalf("capacity classification lost: %v", err)
			}
			if strings.Contains(err.Error(), "waiting alone") == (test.name == "running_requests") {
				t.Fatalf("incorrect recovery guidance: %v", err)
			}
		})
	}
}
