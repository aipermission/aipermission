package connectors

import (
	"errors"
	"fmt"
	"testing"
)

func TestTransferLimitErrorPreservesClassifiedCause(t *testing.T) {
	cause := fmt.Errorf("download stopped: %w", ErrTransferByteLimit)
	classified := ClassifyError("transfer_byte_limit", cause)
	if ErrTransferByteLimit.Error() != "file transfer byte limit exceeded" || !errors.Is(classified, ErrTransferByteLimit) ||
		errors.Is(classified, errors.New(ErrTransferByteLimit.Error())) || ErrorCode(classified) != "transfer_byte_limit" {
		t.Fatalf("transfer limit error identity changed: %v", classified)
	}
}
