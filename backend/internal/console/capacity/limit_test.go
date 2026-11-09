package capacity

import (
	"errors"
	"strings"
	"testing"
)

func TestLimitErrorCarriesBoundedCapacityAndRecoveryHint(t *testing.T) {
	cause := errors.New(LimitMessage)
	err := LimitError{Used: Limit, Cause: cause}
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "32/32") || !strings.Contains(err.Error(), "you own") || !strings.Contains(err.Error(), "Waiting alone") {
		t.Fatal(err)
	}
}
