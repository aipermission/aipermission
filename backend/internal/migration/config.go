package migration

import (
	"fmt"
	"os"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/config"
)

type Config struct {
	Host          string
	Port          string
	DataPath      string
	GatewaySecret string
}

func LoadConfig() (Config, error) {
	dataPath := env("AIPERMISSION_DATA_PATH", "/app/data/aipermission.db")
	secret := strings.TrimSpace(os.Getenv("AIPERMISSION_GATEWAY_SECRET"))
	if secret == "" || secret == "dev-only-change-me" || secret == "change-me-for-local-dev" {
		var err error
		secret, err = config.LoadOrCreateGatewaySecret(dataPath)
		if err != nil {
			return Config{}, err
		}
	}
	cfg := Config{
		Host:          env("AIPERMISSION_MIGRATION_HOST", "0.0.0.0"),
		Port:          env("AIPERMISSION_MIGRATION_PORT", "3211"),
		DataPath:      dataPath,
		GatewaySecret: secret,
	}
	if strings.TrimSpace(cfg.Port) == "" {
		return Config{}, fmt.Errorf("AIPERMISSION_MIGRATION_PORT is required")
	}
	return cfg, nil
}

func (c Config) Address() string {
	return c.Host + ":" + c.Port
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
