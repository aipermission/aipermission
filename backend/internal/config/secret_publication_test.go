package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewaySecretPublicationForcesOneConcurrentLoser(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var created, lost atomic.Int32
	creator := func(path, secret string) (bool, error) {
		arrived <- struct{}{}
		<-release
		won, err := createGatewaySecretExclusive(path, secret)
		if won {
			created.Add(1)
		} else if err == nil {
			lost.Add(1)
		}
		return won, err
	}
	type result struct {
		secret string
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			secret, err := loadOrCreateGatewaySecret(dataPath, creator)
			results <- result{secret, err}
		}()
	}
	// Neither candidate can publish until both callers observed absence.
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("both secret candidates did not reach the publication barrier")
		}
	}
	close(release)
	var winner string
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil || result.secret == "" {
				t.Fatalf("publication result = %#v", result)
			}
			if winner == "" {
				winner = result.secret
			} else if result.secret != winner {
				t.Fatal("concurrent loser did not read the published winner")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("secret publication did not finish")
		}
	}
	if created.Load() != 1 || lost.Load() != 1 {
		t.Fatalf("publication counts: created=%d lost=%d", created.Load(), lost.Load())
	}
	stored, err := LoadOrCreateGatewaySecret(dataPath)
	if err != nil || stored != winner {
		t.Fatalf("public readback did not retain winner: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(dataPath))
	if err != nil || len(entries) != 1 || entries[0].Name() != "gateway.secret" {
		t.Fatalf("candidate cleanup = %v, %v", entries, err)
	}
	info, err := os.Stat(GatewaySecretPath(dataPath))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("published secret mode = %v, %v", info, err)
	}
}

func TestGatewaySecretPublicationFailureDoesNotCreateCredential(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	want := errors.New("publication denied")
	secret, err := loadOrCreateGatewaySecret(dataPath, func(string, string) (bool, error) { return false, want })
	if secret != "" || !errors.Is(err, want) {
		t.Fatalf("failure was not retained: secret length=%d error=%v", len(secret), err)
	}
	if _, err := os.Stat(GatewaySecretPath(dataPath)); !os.IsNotExist(err) {
		t.Fatalf("failed publication created a secret: %v", err)
	}
}

func TestGatewaySecretConcurrentReadCannotAcceptInvalidPublication(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	secret, err := loadOrCreateGatewaySecret(dataPath, func(path, _ string) (bool, error) {
		return false, os.WriteFile(path, []byte("short\n"), 0o600)
	})
	if secret != "" || err == nil || !strings.Contains(err.Error(), "read concurrently created gateway secret") || !strings.Contains(err.Error(), "invalid gateway secret file") {
		t.Fatalf("invalid concurrent publication accepted: secret length=%d error=%v", len(secret), err)
	}
	data, readErr := os.ReadFile(GatewaySecretPath(dataPath))
	if readErr != nil || string(data) != "short\n" {
		t.Fatalf("invalid winner was overwritten: %q, %v", data, readErr)
	}
}
