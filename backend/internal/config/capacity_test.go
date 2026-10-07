package config

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestInvalidConnectorStorageBudgetPreventsStartup(t *testing.T) {
	t.Setenv(actioncapacity.StorageBudgetEnvironment, "unlimited")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), actioncapacity.StorageBudgetEnvironment) {
		t.Fatalf("invalid storage budget did not fail startup: %v", err)
	}
}
