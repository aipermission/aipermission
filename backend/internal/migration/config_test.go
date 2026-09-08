package migration

import (
	"strings"
	"testing"
)

func TestLoadConfigRequiresLoopbackMigrationBind(t *testing.T) {
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "migration-test-secret")
	t.Setenv("AIPERMISSION_MIGRATION_HOST", "0.0.0.0")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "local-only") {
		t.Fatalf("non-loopback migration bind error = %v", err)
	}
}

func TestMigrationAddressSupportsIPv6Loopback(t *testing.T) {
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "migration-test-secret")
	t.Setenv("AIPERMISSION_MIGRATION_HOST", "::1")
	t.Setenv("AIPERMISSION_MIGRATION_PORT", "3211")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load IPv6 migration config: %v", err)
	}
	if got := cfg.Address(); got != "[::1]:3211" {
		t.Fatalf("migration address = %q", got)
	}
}
