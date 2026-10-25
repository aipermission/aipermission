package postgresconnector

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/xdg-go/scram"
)

func testProvisionRoleStatements(target connectors.TargetView, role, password, preset string, scope provisionScope, iterations int) ([]string, map[string]any, error) {
	verifier, err := rolePasswordVerifier(password, iterations)
	if err != nil {
		return nil, nil, err
	}
	return provisionRoleStatements(target, role, verifier, preset, scope)
}

func TestRoleVerifierAuthenticatesWithoutPlaintextInStatements(t *testing.T) {
	const password = "fixture-password-only"
	verifier, err := rolePasswordVerifier(password, 8192)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rolePasswordVerifier(password, 8192)
	if err != nil || other == verifier {
		t.Fatal("password verifiers must use independent salts")
	}
	parts := strings.Split(string(verifier), "$")
	factors := strings.Split(parts[1], ":")
	keys := strings.Split(parts[2], ":")
	salt, _ := base64.StdEncoding.DecodeString(factors[1])
	storedKey, _ := base64.StdEncoding.DecodeString(keys[0])
	serverKey, _ := base64.StdEncoding.DecodeString(keys[1])
	iterations, _ := strconv.Atoi(factors[0])
	server, err := scram.SHA256.NewServer(func(string) (scram.StoredCredentials, error) {
		return scram.StoredCredentials{KeyFactors: scram.KeyFactors{Salt: string(salt), Iters: iterations}, StoredKey: storedKey, ServerKey: serverKey}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := scram.SHA256.NewClient("reader", password, "")
	clientConversation, serverConversation := client.NewConversation(), server.NewConversation()
	message, err := clientConversation.Step("")
	for !clientConversation.Done() && err == nil {
		message, err = serverConversation.Step(message)
		if err == nil {
			message, err = clientConversation.Step(message)
		}
	}
	if err != nil || !clientConversation.Valid() || !serverConversation.Valid() {
		t.Fatalf("verifier authentication failed: %v", err)
	}
	statements, _, err := provisionRoleStatements(connectors.TargetView{Config: map[string]any{"database": "appdb"}}, "reader", verifier, "read_only", provisionScope{AllSchemas: true})
	if err != nil || strings.Contains(strings.Join(statements, "\n"), password) || !strings.Contains(statements[0], string(verifier)) {
		t.Fatalf("plaintext or missing verifier in role statements: %v", err)
	}
}

func TestRoleVerifierRejectsUnsafeWorkFactors(t *testing.T) {
	for _, iterations := range []int{-1, 0, 4095, (1 << 20) + 1} {
		t.Run(fmt.Sprint(iterations), func(t *testing.T) {
			if _, err := rolePasswordVerifier("fixture", iterations); err == nil {
				t.Fatal("unsafe or unbounded work factor accepted")
			}
		})
	}
}
