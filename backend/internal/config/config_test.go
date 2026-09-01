package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCreatesAndReusesGatewaySecretWhenDefaultConfigured(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	t.Setenv("AIPERMISSION_DATA_PATH", dataPath)
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "dev-only-change-me")
	t.Setenv("AIPERMISSION_FRONTEND_PORT", "3333")
	t.Setenv("AIPERMISSION_ALLOWED_ORIGINS", "")

	first, err := Load()
	if err != nil {
		t.Fatalf("load first config: %v", err)
	}
	if first.GatewaySecret == "" || first.GatewaySecret == "dev-only-change-me" {
		t.Fatalf("expected generated gateway secret, got %q", first.GatewaySecret)
	}
	if first.Address() != "127.0.0.1:8080" {
		t.Fatalf("unexpected address: %s", first.Address())
	}
	if got := first.AllowedOrigins; len(got) != 2 || got[0] != "http://localhost:3333" || got[1] != "http://127.0.0.1:3333" {
		t.Fatalf("unexpected origins from frontend port: %#v", got)
	}
	if first.PublicStatus()["gateway_secret"] != "configured" {
		t.Fatalf("public status should not expose secret: %#v", first.PublicStatus())
	}
	if _, ok := first.PublicStatus()["remote_access"]; ok {
		t.Fatalf("public status should not expose remote access mode: %#v", first.PublicStatus())
	}

	second, err := Load()
	if err != nil {
		t.Fatalf("load second config: %v", err)
	}
	if second.GatewaySecret != first.GatewaySecret {
		t.Fatalf("gateway secret should be reused")
	}
	info, err := os.Stat(GatewaySecretPath(dataPath))
	if err != nil {
		t.Fatalf("stat gateway secret: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("gateway secret permissions = %o", info.Mode().Perm())
	}
}

func TestLoadDefaultsCORSOriginsToFrontendPort3210(t *testing.T) {
	t.Setenv("AIPERMISSION_DATA_PATH", filepath.Join(t.TempDir(), "aipermission.db"))
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "real-secret-with-at-least-32-characters")
	t.Setenv("AIPERMISSION_ALLOWED_ORIGINS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.AllowedOrigins; len(got) != 2 || got[0] != "http://localhost:3210" || got[1] != "http://127.0.0.1:3210" {
		t.Fatalf("unexpected default origins: %#v", got)
	}
}

func TestLoadHonorsExplicitEnv(t *testing.T) {
	t.Setenv("AIPERMISSION_BACKEND_HOST", "localhost")
	t.Setenv("AIPERMISSION_BACKEND_PORT", "9000")
	t.Setenv("AIPERMISSION_DATA_PATH", filepath.Join(t.TempDir(), "custom.db"))
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "real-secret-with-at-least-32-characters")
	t.Setenv("AIPERMISSION_ALLOWED_ORIGINS", " http://localhost:9001, ,http://127.0.0.1:9001,https://[::1]:9001 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Address() != "localhost:9000" {
		t.Fatalf("unexpected address: %s", cfg.Address())
	}
	if got := cfg.AllowedOrigins; len(got) != 3 || got[0] != "http://localhost:9001" || got[1] != "http://127.0.0.1:9001" || got[2] != "https://[::1]:9001" {
		t.Fatalf("unexpected origins: %#v", got)
	}
	if _, err := os.Stat(GatewaySecretPath(cfg.DataPath)); !os.IsNotExist(err) {
		t.Fatalf("explicit gateway secret should not create gateway.secret, err=%v", err)
	}
}

func TestLoadRejectsNonLoopbackAllowedOrigin(t *testing.T) {
	t.Setenv("AIPERMISSION_DATA_PATH", filepath.Join(t.TempDir(), "custom.db"))
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "real-secret-with-at-least-32-characters")
	t.Setenv("AIPERMISSION_ALLOWED_ORIGINS", "https://example.com")

	if _, err := Load(); err == nil {
		t.Fatalf("expected non-loopback allowed origin to fail")
	}
}

func TestLoadRejectsNonLocalBind(t *testing.T) {
	t.Setenv("AIPERMISSION_BACKEND_HOST", "0.0.0.0")
	t.Setenv("AIPERMISSION_DATA_PATH", filepath.Join(t.TempDir(), "custom.db"))
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "real-secret-with-at-least-32-characters")

	if _, err := Load(); err == nil {
		t.Fatalf("expected non-local bind to fail")
	}
}

func TestLoadRejectsWeakExplicitGatewaySecret(t *testing.T) {
	t.Setenv("AIPERMISSION_DATA_PATH", filepath.Join(t.TempDir(), "custom.db"))
	t.Setenv("AIPERMISSION_GATEWAY_SECRET", "short")

	if _, err := Load(); err == nil {
		t.Fatalf("expected weak explicit gateway secret to fail")
	}
}

func TestLoadOrCreateGatewaySecretRejectsInvalidExistingFileWithoutReplacingIt(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	path := GatewaySecretPath(dataPath)
	if err := os.WriteFile(path, []byte("short\n"), 0o600); err != nil {
		t.Fatalf("write invalid gateway secret: %v", err)
	}

	if _, err := LoadOrCreateGatewaySecret(dataPath); err == nil || !strings.Contains(err.Error(), "invalid gateway secret file") {
		t.Fatalf("expected invalid existing gateway secret error, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read invalid gateway secret after failure: %v", err)
	}
	if string(data) != "short\n" {
		t.Fatalf("invalid gateway secret was replaced: %q", data)
	}
}

func TestLoadOrCreateGatewaySecretRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "aipermission.db")
	target := filepath.Join(dir, "elsewhere.secret")
	secret := strings.Repeat("s", 32)
	if err := os.WriteFile(target, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(target, GatewaySecretPath(dataPath)); err != nil {
		t.Fatalf("create gateway secret symlink: %v", err)
	}

	if _, err := LoadOrCreateGatewaySecret(dataPath); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestLoadOrCreateGatewaySecretSecuresAndReusesLegacyCustomValue(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	path := GatewaySecretPath(dataPath)
	secret := "legacy-custom-gateway-secret-value-123456"
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy gateway secret: %v", err)
	}

	got, err := LoadOrCreateGatewaySecret(dataPath)
	if err != nil {
		t.Fatalf("load legacy gateway secret: %v", err)
	}
	if got != secret {
		t.Fatalf("gateway secret = %q, want %q", got, secret)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat gateway secret: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("gateway secret permissions = %o", info.Mode().Perm())
	}
}

func TestSaveGatewaySecretWritesAtomicallyWithPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	dataPath := filepath.Join(dir, "aipermission.db")
	secret := strings.Repeat("a", 32)

	if err := SaveGatewaySecret(dataPath, secret); err != nil {
		t.Fatalf("save gateway secret: %v", err)
	}
	data, err := os.ReadFile(GatewaySecretPath(dataPath))
	if err != nil {
		t.Fatalf("read gateway secret: %v", err)
	}
	if string(data) != secret+"\n" {
		t.Fatalf("gateway secret contents = %q", data)
	}
	info, err := os.Stat(GatewaySecretPath(dataPath))
	if err != nil {
		t.Fatalf("stat gateway secret: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("gateway secret permissions = %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat gateway secret directory: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("gateway secret directory permissions = %o", dirInfo.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".gateway.secret.tmp-*"))
	if err != nil {
		t.Fatalf("glob gateway secret replacements: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary gateway secret files remain: %v", matches)
	}
}

func TestSaveGatewaySecretPreservesExistingDirectoryPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared-data")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("create existing data directory: %v", err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatalf("set existing data directory permissions: %v", err)
	}

	if err := SaveGatewaySecret(filepath.Join(dir, "aipermission.db"), strings.Repeat("s", 32)); err != nil {
		t.Fatalf("save gateway secret: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat existing data directory: %v", err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("existing data directory permissions = %o", info.Mode().Perm())
	}
}

func TestSaveGatewaySecretRejectsNonRegularDestination(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	if err := os.Mkdir(GatewaySecretPath(dataPath), 0o700); err != nil {
		t.Fatalf("create gateway secret directory destination: %v", err)
	}

	if err := SaveGatewaySecret(dataPath, strings.Repeat("s", 32)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected non-regular destination rejection, got %v", err)
	}
}
