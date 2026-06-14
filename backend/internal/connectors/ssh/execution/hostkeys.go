package execution

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var knownHostsMu sync.Mutex

type UnknownHostKeyError struct {
	Hostname          string `json:"hostname"`
	KeyType           string `json:"key_type"`
	FingerprintSHA256 string `json:"fingerprint_sha256"`
	PublicKey         string `json:"public_key"`
}

func (err *UnknownHostKeyError) Error() string {
	return fmt.Sprintf("ssh host key approval required for %s (%s)", err.Hostname, err.FingerprintSHA256)
}

type ChangedHostKeyError struct {
	Hostname             string   `json:"hostname"`
	KeyType              string   `json:"key_type"`
	FingerprintSHA256    string   `json:"fingerprint_sha256"`
	PublicKey            string   `json:"public_key"`
	ExistingFingerprints []string `json:"existing_fingerprints"`
}

func (err *ChangedHostKeyError) Error() string {
	return fmt.Sprintf("ssh host key changed for %s (%s)", err.Hostname, err.FingerprintSHA256)
}

func HostKeyCallback(path string) (ssh.HostKeyCallback, error) {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return nil, fmt.Errorf("known_hosts path is required")
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()

		remote = knownHostsRemoteAddr(remote)
		if err := ensureKnownHostsFile(path); err != nil {
			return err
		}
		callback, err := knownhosts.New(path)
		if err != nil {
			return fmt.Errorf("load known_hosts: %w", err)
		}
		if err := callback(hostname, remote, key); err == nil {
			return nil
		} else {
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) && len(keyErr.Want) > 0 {
				return NewChangedHostKeyError(hostname, key, keyErr.Want)
			}
			if !errors.As(err, &keyErr) {
				return fmt.Errorf("verify ssh host key: %w", err)
			}
		}

		return NewUnknownHostKeyError(hostname, key)
	}, nil
}

func NewChangedHostKeyError(hostname string, key ssh.PublicKey, existing []knownhosts.KnownKey) *ChangedHostKeyError {
	fingerprints := make([]string, 0, len(existing))
	seen := map[string]bool{}
	for _, item := range existing {
		fingerprint := HostKeyFingerprintSHA256(item.Key)
		if fingerprint == "" || seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		fingerprints = append(fingerprints, fingerprint)
	}
	return &ChangedHostKeyError{
		Hostname:             hostname,
		KeyType:              key.Type(),
		FingerprintSHA256:    HostKeyFingerprintSHA256(key),
		PublicKey:            base64.StdEncoding.EncodeToString(key.Marshal()),
		ExistingFingerprints: fingerprints,
	}
}

func NewUnknownHostKeyError(hostname string, key ssh.PublicKey) *UnknownHostKeyError {
	return &UnknownHostKeyError{
		Hostname:          hostname,
		KeyType:           key.Type(),
		FingerprintSHA256: HostKeyFingerprintSHA256(key),
		PublicKey:         base64.StdEncoding.EncodeToString(key.Marshal()),
	}
}

func ParseHostPublicKey(publicKey string) (ssh.PublicKey, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil {
		return nil, fmt.Errorf("decode host public key: %w", err)
	}
	key, err := ssh.ParsePublicKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse host public key: %w", err)
	}
	return key, nil
}

func HostKeyFingerprintSHA256(key ssh.PublicKey) string {
	return ssh.FingerprintSHA256(key)
}

func TrustHostKey(path string, hostname string, publicKey string) error {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return fmt.Errorf("known_hosts path is required")
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return fmt.Errorf("hostname is required")
	}
	key, err := ParseHostPublicKey(publicKey)
	if err != nil {
		return err
	}

	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()

	if err := ensureKnownHostsFile(path); err != nil {
		return err
	}
	callback, err := knownhosts.New(path)
	if err != nil {
		return fmt.Errorf("load known_hosts: %w", err)
	}
	if err := callback(hostname, knownHostsRemoteAddr(nil), key); err == nil {
		return nil
	} else {
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
			return fmt.Errorf("verify ssh host key: %w", err)
		}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer file.Close()

	if _, err := fmt.Fprintln(file, knownhosts.Line([]string{hostname}, key)); err != nil {
		return fmt.Errorf("append known_hosts: %w", err)
	}
	return nil
}

func ReplaceHostKey(path string, hostname string, publicKey string) error {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return fmt.Errorf("known_hosts path is required")
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return fmt.Errorf("hostname is required")
	}

	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()

	if err := ensureKnownHostsFile(path); err != nil {
		return err
	}
	if err := removeKnownHostLine(path, hostname); err != nil {
		return err
	}
	key, err := ParseHostPublicKey(publicKey)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer file.Close()
	if _, err := fmt.Fprintln(file, knownhosts.Line([]string{hostname}, key)); err != nil {
		return fmt.Errorf("append known_hosts: %w", err)
	}
	return nil
}

func removeKnownHostLine(path string, hostname string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read known_hosts: %w", err)
	}
	matches := knownHostNameSet(hostname)
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		remove := false
		for _, name := range strings.Split(fields[0], ",") {
			if matches[name] {
				remove = true
				break
			}
		}
		if !remove {
			kept = append(kept, line)
		}
	}
	output := strings.Join(kept, "\n")
	if output != "" {
		output += "\n"
	}
	if err := os.WriteFile(path, []byte(output), 0o600); err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}
	return nil
}

func knownHostNameSet(hostname string) map[string]bool {
	hostname = strings.TrimSpace(hostname)
	values := map[string]bool{hostname: true}
	host, port, err := net.SplitHostPort(hostname)
	if err == nil {
		values[host] = true
		values[net.JoinHostPort(host, port)] = true
		if port == "22" {
			values[host] = true
		}
	}
	return values
}

func knownHostsRemoteAddr(remote net.Addr) net.Addr {
	if remote != nil {
		return remote
	}
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
}

func ensureKnownHostsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create known_hosts directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	return file.Close()
}
