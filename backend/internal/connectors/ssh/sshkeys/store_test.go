package sshkeys

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/pem"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"golang.org/x/crypto/ssh"
)

func openSSHKeyTestStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("gateway-secret")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	return database, NewStore(database, secretVault)
}

func TestInstallCommandShellQuotesPublicKey(t *testing.T) {
	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA-test aipermission-main'$(touch /tmp/pwned)"
	command := InstallCommand(publicKey)

	if strings.Contains(command, `echo "`) {
		t.Fatalf("install command should not use double-quoted echo: %s", command)
	}
	if !strings.Contains(command, `printf '%s\n'`) {
		t.Fatalf("install command should use printf: %s", command)
	}
	if !strings.Contains(command, `'\''`) {
		t.Fatalf("install command should escape single quotes: %s", command)
	}
}

func TestSSHKeyStoreCreateListGetPrivateKeyAndDelete(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	created, err := store.Create(ctx, CreateRequest{Name: " main key ", KeyType: ""})
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	if created.Name != "main key" || created.KeyType != TypeED25519 {
		t.Fatalf("unexpected created key: %#v", created)
	}
	if !strings.HasPrefix(created.PublicKey, "ssh-ed25519 ") || !strings.Contains(created.PublicKey, "aipermission-main key") {
		t.Fatalf("unexpected public key: %s", created.PublicKey)
	}
	if created.InstallCommand == "" || !strings.Contains(created.InstallCommand, created.PublicKey) {
		t.Fatalf("install command should include public key")
	}

	private, err := store.GetPrivateKey(ctx, created.ID)
	if err != nil {
		t.Fatalf("get private key: %v", err)
	}
	if private.PrivateKey == "" || !strings.Contains(private.PrivateKey, "PRIVATE KEY") {
		t.Fatalf("private key was not decrypted: %#v", private)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("unexpected list: %#v", list)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSSHKeyStoreCreatesRSAKey(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	created, err := store.Create(ctx, CreateRequest{Name: "rsa", KeyType: TypeRSA})
	if err != nil {
		t.Fatalf("create rsa key: %v", err)
	}
	if created.KeyType != TypeRSA || !strings.HasPrefix(created.PublicKey, "ssh-rsa ") {
		t.Fatalf("unexpected rsa key: %#v", created)
	}
}

func TestSSHKeyStoreImportsExistingPrivateKey(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	source, err := store.Create(ctx, CreateRequest{Name: "source", KeyType: TypeED25519})
	if err != nil {
		t.Fatalf("create source key: %v", err)
	}
	sourcePrivate, err := store.GetPrivateKey(ctx, source.ID)
	if err != nil {
		t.Fatalf("get source private key: %v", err)
	}

	imported, err := store.Import(ctx, ImportRequest{Name: "imported", PrivateKey: sourcePrivate.PrivateKey})
	if err != nil {
		t.Fatalf("import private key: %v", err)
	}
	if imported.Name != "imported" || imported.KeyType != TypeED25519 {
		t.Fatalf("unexpected imported key: %#v", imported)
	}
	if imported.Fingerprint != source.Fingerprint {
		t.Fatalf("import should preserve fingerprint, got %q want %q", imported.Fingerprint, source.Fingerprint)
	}
	if strings.Contains(imported.PublicKey, "source") || !strings.Contains(imported.PublicKey, "aipermission-imported") {
		t.Fatalf("import should rewrite public key comment: %s", imported.PublicKey)
	}

	private, err := store.GetPrivateKey(ctx, imported.ID)
	if err != nil {
		t.Fatalf("get imported private key: %v", err)
	}
	if _, err := ssh.ParsePrivateKey([]byte(private.PrivateKey)); err != nil {
		t.Fatalf("stored imported private key should be parseable without passphrase: %v", err)
	}
}

func TestSSHKeyStoreImportsECDSAPrivateKey(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(private, "test-ecdsa")
	if err != nil {
		t.Fatalf("marshal ecdsa key: %v", err)
	}
	privateKey := string(pem.EncodeToMemory(block))

	imported, err := store.Import(ctx, ImportRequest{Name: "ecdsa", PrivateKey: privateKey})
	if err != nil {
		t.Fatalf("import ecdsa private key: %v", err)
	}
	if imported.KeyType != TypeECDSA || !strings.HasPrefix(imported.PublicKey, "ecdsa-sha2-nistp256 ") {
		t.Fatalf("unexpected imported ecdsa key: %#v", imported)
	}
}

func TestSSHKeyStoreRejectsWeakRSAImport(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	private, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate weak rsa key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(private, "weak-rsa")
	if err != nil {
		t.Fatalf("marshal weak rsa key: %v", err)
	}

	_, err = store.Import(ctx, ImportRequest{Name: "weak-rsa", PrivateKey: string(pem.EncodeToMemory(block))})
	if err == nil || !strings.Contains(err.Error(), "at least 2048 bits") {
		t.Fatalf("expected weak rsa import to fail, got %v", err)
	}
}

func TestSSHKeyStoreImportsPassphraseProtectedPrivateKey(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "test-key", []byte("correct horse"))
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}
	encryptedPrivateKey := string(pem.EncodeToMemory(block))
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	expectedFingerprint := ssh.FingerprintSHA256(sshPublic)

	if _, err := store.Import(ctx, ImportRequest{Name: "missing-passphrase", PrivateKey: encryptedPrivateKey}); err == nil {
		t.Fatalf("expected encrypted key import without passphrase to fail")
	}
	imported, err := store.Import(ctx, ImportRequest{Name: "encrypted", PrivateKey: encryptedPrivateKey, Passphrase: "correct horse"})
	if err != nil {
		t.Fatalf("import encrypted private key: %v", err)
	}
	if imported.Fingerprint != expectedFingerprint {
		t.Fatalf("unexpected fingerprint: got %q want %q", imported.Fingerprint, expectedFingerprint)
	}

	privateKey, err := store.GetPrivateKey(ctx, imported.ID)
	if err != nil {
		t.Fatalf("get private key: %v", err)
	}
	if strings.Contains(privateKey.PrivateKey, "ENCRYPTED") {
		t.Fatalf("stored key should be normalized into the local encrypted vault, not kept passphrase-encrypted")
	}
	if _, err := ssh.ParsePrivateKey([]byte(privateKey.PrivateKey)); err != nil {
		t.Fatalf("stored private key should be usable without original passphrase: %v", err)
	}
}

func TestSSHKeyStoreDuplicateNameReturnsValidationError(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	if _, err := store.Create(ctx, CreateRequest{Name: "main", KeyType: TypeED25519}); err != nil {
		t.Fatalf("create first key: %v", err)
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "main", KeyType: TypeED25519}); !errors.As(err, new(ValidationError)) {
		t.Fatalf("expected duplicate key validation error, got %v", err)
	}
}

func TestSSHKeyStoreUpdatesCredentialName(t *testing.T) {
	ctx := context.Background()
	_, store := openSSHKeyTestStore(t)

	key, err := store.Create(ctx, CreateRequest{Name: "main", KeyType: TypeED25519})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	updated, err := store.Update(ctx, key.ID, UpdateRequest{Name: "ops"})
	if err != nil {
		t.Fatalf("update key: %v", err)
	}
	if updated.Name != "ops" || !strings.Contains(updated.PublicKey, "aipermission-ops") || !strings.Contains(updated.InstallCommand, "aipermission-ops") {
		t.Fatalf("expected updated key name and public comment, got %#v", updated)
	}

	if _, err := store.Create(ctx, CreateRequest{Name: "existing", KeyType: TypeED25519}); err != nil {
		t.Fatalf("create existing key: %v", err)
	}
	if _, err := store.Update(ctx, key.ID, UpdateRequest{Name: "existing"}); !errors.As(err, new(ValidationError)) {
		t.Fatalf("expected duplicate update validation error, got %v", err)
	}
	if _, err := store.Update(ctx, 999, UpdateRequest{Name: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing update to return ErrNotFound, got %v", err)
	}
}

func TestSSHKeyStoreValidatesAndRefusesDeleteWhenInUse(t *testing.T) {
	ctx := context.Background()
	database, store := openSSHKeyTestStore(t)

	if ValidationError("bad key").Error() != "bad key" {
		t.Fatalf("validation error should return message")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "", KeyType: TypeED25519}); err == nil {
		t.Fatalf("expected empty name to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "bad\nname", KeyType: TypeED25519}); err == nil {
		t.Fatalf("expected multiline name to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: strings.Repeat("a", 81), KeyType: TypeED25519}); err == nil {
		t.Fatalf("expected long name to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "bad", KeyType: "dsa"}); err == nil {
		t.Fatalf("expected invalid key type to fail")
	}
	if _, err := store.Get(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := store.GetPrivateKey(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for private key, got %v", err)
	}

	key, err := store.Create(ctx, CreateRequest{Name: "used", KeyType: TypeED25519})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	targetResult, err := database.Exec(`
		INSERT INTO connector_targets (connector_kind, name, config_json, created_at, updated_at)
		VALUES ('ssh', 'worker-1', '{"host":"127.0.0.1","port":22}', ?, ?)`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert connector target: %v", err)
	}
	targetID, err := targetResult.LastInsertId()
	if err != nil {
		t.Fatalf("read connector target id: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at)
		VALUES (?, 'ssh', 'private_key', 'root', ?, '', ?, ?)`,
		targetID,
		`{"username":"root","ssh_key_id":`+strconv.FormatInt(key.ID, 10)+`}`,
		now,
		now,
	); err != nil {
		t.Fatalf("insert connector profile: %v", err)
	}
	if err := store.Delete(ctx, key.ID); err == nil {
		t.Fatalf("expected delete to fail while key is in use")
	}
	if _, err := database.Exec(`UPDATE connector_credential_profiles SET status = 'archived', updated_at = ? WHERE target_id = ?`, now, targetID); err != nil {
		t.Fatalf("archive connector profile: %v", err)
	}
	if err := store.Delete(ctx, key.ID); err != nil {
		t.Fatalf("archived profile should not block key delete: %v", err)
	}
}
