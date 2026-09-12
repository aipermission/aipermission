package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const testSSHKeyTypeED25519 = "ed25519"

type testSSHKey struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	KeyType        string `json:"key_type"`
	PublicKey      string `json:"public_key"`
	Fingerprint    string `json:"fingerprint"`
	InstallCommand string `json:"install_command"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type testSSHKeyCreateRequest struct {
	Name    string `json:"name"`
	KeyType string `json:"key_type"`
}

type testSSHKeyImportRequest struct {
	Name       string `json:"name"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}

type testSSHKeyUpdateRequest struct {
	Name string `json:"name"`
}

type testSSHPrivateKey struct {
	PrivateKey string `json:"private_key"`
}

type testSSHKeyResourceStore struct {
	resources connectorapi.CredentialResourceStore
}

func newTestSSHKeyStore(resources connectorapi.CredentialResourceStore) *testSSHKeyResourceStore {
	return &testSSHKeyResourceStore{resources: resources}
}

func (store *testSSHKeyResourceStore) Create(ctx context.Context, request testSSHKeyCreateRequest) (testSSHKey, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return testSSHKey{}, err
	}
	comment := "aipermission-" + request.Name
	privateBlock, err := ssh.MarshalPrivateKey(private, comment)
	if err != nil {
		return testSSHKey{}, err
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return testSSHKey{}, err
	}
	publicText := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))) + " " + comment
	record, err := store.resources.Create(ctx, connectorapi.CreateCredentialResourceInput{
		Name: request.Name, ResourceType: testSSHKeyTypeED25519,
		PublicData: publicText, Fingerprint: ssh.FingerprintSHA256(sshPublic),
		Secret: testSSHPrivateKey{PrivateKey: string(pem.EncodeToMemory(privateBlock))},
	})
	return testSSHKeyFromResource(record), err
}

func (store *testSSHKeyResourceStore) Get(ctx context.Context, id int64) (testSSHKey, error) {
	record, err := store.resources.Get(ctx, id)
	return testSSHKeyFromResource(record), err
}

func (store *testSSHKeyResourceStore) GetPrivateKey(ctx context.Context, id int64) (testSSHPrivateKey, error) {
	var secret testSSHPrivateKey
	err := store.resources.GetSecret(ctx, id, &secret)
	return secret, err
}

func testSSHKeyFromResource(record connectorapi.CredentialResource) testSSHKey {
	return testSSHKey{
		ID: record.ID, Name: record.Name, KeyType: record.ResourceType,
		PublicKey: record.PublicData, Fingerprint: record.Fingerprint,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func trustTestSSHHostKey(path, hostname, encodedPublicKey string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedPublicKey))
	if err != nil {
		return err
	}
	key, err := ssh.ParsePublicKey(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(knownhosts.Line([]string{hostname}, key)+"\n"), 0o600)
}

func trustedTestSSHHostFingerprints(path, hostname string) ([]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fingerprints []string
	normalizedHostname := knownhosts.Normalize(hostname)
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !containsKnownHost(fields[0], normalizedHostname) {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(fields[2])
		if err != nil {
			return nil, err
		}
		key, err := ssh.ParsePublicKey(raw)
		if err != nil {
			return nil, err
		}
		fingerprints = append(fingerprints, ssh.FingerprintSHA256(key))
	}
	if len(fingerprints) == 0 {
		return nil, fmt.Errorf("no trusted test host key exists for %s", hostname)
	}
	return fingerprints, nil
}

func containsKnownHost(field, hostname string) bool {
	for _, candidate := range strings.Split(field, ",") {
		if candidate == hostname {
			return true
		}
	}
	return false
}
