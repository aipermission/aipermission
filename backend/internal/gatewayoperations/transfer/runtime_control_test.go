package transfer

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func TestBatchControlReturnsNilWhenNoControlIsRegistered(t *testing.T) {
	runtime := &Runtime{jobs: &transferjobs.Registry{}}
	if control := runtime.BatchControl(42); control != nil {
		t.Fatalf("batch control = %#v, want nil", control)
	}
}
