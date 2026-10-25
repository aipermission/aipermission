package postgresconnector

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/cleanup"
	"github.com/xdg-go/scram"
)

func roleSCRAMIterations(ctx context.Context, runtime connectors.RuntimeContext) (int, error) {
	connection, err := connect(ctx, runtime)
	if err != nil {
		return 0, err
	}
	defer func() { _ = cleanup.Run(ctx, connection.Close) }()
	var setting string
	if err := connection.QueryRow(ctx, `SELECT COALESCE(current_setting('scram_iterations', true), '4096')`).Scan(&setting); err != nil {
		return 0, fmt.Errorf("read managed role password policy: %w", err)
	}
	iterations, err := strconv.Atoi(setting)
	if err != nil || iterations < 4096 || iterations > 1<<20 {
		return 0, fmt.Errorf("managed role SCRAM iterations must be between 4096 and 1048576")
	}
	return iterations, nil
}

type roleVerifier string

func rolePasswordVerifier(password string, iterations int) (roleVerifier, error) {
	if iterations < 4096 || iterations > 1<<20 {
		return "", fmt.Errorf("managed role SCRAM iterations must be between 4096 and 1048576")
	}
	client, err := scram.SHA256.NewClient("", password, "")
	if err != nil {
		return "", fmt.Errorf("prepare managed role password: %w", err)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate managed role password salt: %w", err)
	}
	credentials, err := client.GetStoredCredentialsWithError(scram.KeyFactors{Salt: string(salt), Iters: iterations})
	if err != nil {
		return "", fmt.Errorf("derive managed role password verifier: %w", err)
	}
	encode := base64.StdEncoding.EncodeToString
	return roleVerifier(fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, encode(salt), encode(credentials.StoredKey), encode(credentials.ServerKey))), nil
}
