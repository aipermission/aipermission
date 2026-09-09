package api

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
)

func TestRuntimeConfigurationSnapshotPreservesDynamicGatewaySecretStatus(t *testing.T) {
	snapshot := snapshotRuntimeConfiguration(config.Config{
		Host: "127.0.0.1", Port: "8080", FrontendPort: "3210",
		DataPath: "/tmp/aipermission.db", GatewaySecret: "dev-only-change-me",
	})
	if got := snapshot.PublicStatusMinimal()["gateway_secret"]; got != "development-default" {
		t.Fatalf("initial gateway secret state = %v", got)
	}
	snapshot.GatewaySecret = "a-runtime-secret-with-more-than-thirty-two-bytes"
	if got := snapshot.PublicStatusMinimal()["gateway_secret"]; got != "configured" {
		t.Fatalf("updated gateway secret state = %v", got)
	}
}

func TestRuntimeConfigurationSnapshotFailsClosedWithoutSource(t *testing.T) {
	snapshot := snapshotRuntimeConfiguration(nil)
	if snapshot.AllowsOrigin("http://localhost:3210") ||
		snapshot.IsLocalhostHeader("localhost:3210") ||
		snapshot.IsLocalRemoteAddr("127.0.0.1:1234") {
		t.Fatal("missing runtime configuration must fail closed")
	}
}
