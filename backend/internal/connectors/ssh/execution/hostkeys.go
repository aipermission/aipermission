package execution

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- OpenSSH known_hosts hashing is fixed to HMAC-SHA1.
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var knownHostsMu sync.Mutex

var errKnownHostsTrustStateIndeterminate = errors.New("SSH known_hosts trust state is indeterminate")

type knownHostsFileOps struct {
	rename  func(string, string) error
	syncDir func(string) error
}

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

func TrustedHostFingerprints(path string, hostname string) ([]string, error) {
	path = filepath.Clean(path)
	hostname = strings.TrimSpace(hostname)
	if path == "." || path == "" || hostname == "" {
		return nil, fmt.Errorf("known_hosts path and hostname are required")
	}
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	if err := ensureKnownHostsFile(path); err != nil {
		return nil, err
	}
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	probe, err := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		return nil, fmt.Errorf("create host key probe: %w", err)
	}
	verifyErr := callback(hostname, knownHostsRemoteAddr(nil), probe)
	wanted := []knownhosts.KnownKey{}
	if verifyErr == nil {
		wanted = append(wanted, knownhosts.KnownKey{Key: probe})
	} else {
		var keyErr *knownhosts.KeyError
		if !errors.As(verifyErr, &keyErr) {
			return nil, fmt.Errorf("verify known_hosts entry: %w", verifyErr)
		}
		wanted = keyErr.Want
	}
	seen := map[string]bool{}
	fingerprints := []string{}
	for _, item := range wanted {
		if item.Key == nil {
			continue
		}
		fingerprint := HostKeyFingerprintSHA256(item.Key)
		if fingerprint != "" && !seen[fingerprint] {
			seen[fingerprint] = true
			fingerprints = append(fingerprints, fingerprint)
		}
	}
	sort.Strings(fingerprints)
	if len(fingerprints) == 0 {
		return nil, fmt.Errorf("no trusted SSH host key exists for %s", hostname)
	}
	return fingerprints, nil
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
	key, err := ParseHostPublicKey(publicKey)
	if err != nil {
		return err
	}

	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()

	if err := ensureKnownHostsFile(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read known_hosts: %w", err)
	}
	previousKeys, err := acceptedHostKeys(path, data, hostname)
	if err != nil {
		return err
	}
	output := replaceKnownHostData(data, hostname, knownhosts.Line([]string{hostname}, key))
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat known_hosts: %w", err)
	}
	return writeKnownHostsAtomically(path, output, info.Mode().Perm(), func(candidatePath string) error {
		return validateHostKeyReplacement(candidatePath, hostname, key, previousKeys)
	})
}

func replaceKnownHostData(data []byte, hostname, replacement string) []byte {
	target := knownhosts.Normalize(strings.TrimSpace(hostname))
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			kept = append(kept, line)
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			kept = append(kept, line)
			continue
		}
		if strings.HasPrefix(fields[0], "@") {
			// Marker records carry OpenSSH security semantics that must survive a
			// normal host-key replacement. In particular, removing @revoked can
			// silently reactivate a key that is also trusted by a wildcard entry.
			kept = append(kept, line)
			continue
		}
		hostField := 0
		names := strings.Split(fields[hostField], ",")
		remaining := names[:0]
		removed := false
		for _, name := range names {
			if knownHostPatternMatches(name, target) {
				removed = true
				continue
			}
			remaining = append(remaining, name)
		}
		if !removed {
			kept = append(kept, line)
			continue
		}
		if len(remaining) == 0 {
			continue
		}
		fields[hostField] = strings.Join(remaining, ",")
		kept = append(kept, strings.Join(fields, " "))
	}
	kept = append(kept, replacement)
	return []byte(strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n")
}

func writeKnownHostsAtomically(path string, data []byte, mode os.FileMode, validate func(string) error) (err error) {
	return writeKnownHostsAtomicallyWithOps(path, data, mode, validate, knownHostsFileOps{
		rename:  renameKnownHostsFile,
		syncDir: syncKnownHostsDirectory,
	})
}

func writeKnownHostsAtomicallyWithOps(path string, data []byte, mode os.FileMode, validate func(string) error, ops knownHostsFileOps) (err error) {
	dir := filepath.Dir(path)
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read known_hosts before replacement: %w", err)
	}
	rollbackPath, err := writeKnownHostsCandidate(dir, "."+filepath.Base(path)+".rollback-*", original, mode)
	if err != nil {
		return fmt.Errorf("prepare known_hosts rollback: %w", err)
	}
	defer os.Remove(rollbackPath)

	tempPath, err := writeKnownHostsCandidate(dir, "."+filepath.Base(path)+".tmp-*", data, mode)
	if err != nil {
		return fmt.Errorf("prepare known_hosts replacement: %w", err)
	}
	defer os.Remove(tempPath)
	if validate != nil {
		if err = validate(tempPath); err != nil {
			return err
		}
	}
	if err = ops.rename(tempPath, path); err != nil {
		return fmt.Errorf("replace known_hosts: %w", err)
	}
	if err = ops.syncDir(dir); err != nil {
		syncErr := fmt.Errorf("sync known_hosts directory: %w", err)
		if rollbackErr := restoreKnownHostsReplacement(path, rollbackPath, dir, ops); rollbackErr != nil {
			return errors.Join(errKnownHostsTrustStateIndeterminate, syncErr, rollbackErr)
		}
		return syncErr
	}
	return nil
}

func writeKnownHostsCandidate(dir, pattern string, data []byte, mode os.FileMode) (path string, err error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err = file.Write(data); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	err = file.Close()
	file = nil
	return path, err
}

func restoreKnownHostsReplacement(path, rollbackPath, dir string, ops knownHostsFileOps) error {
	if err := ops.rename(rollbackPath, path); err != nil {
		return fmt.Errorf("restore previous known_hosts: %w", err)
	}
	if err := ops.syncDir(dir); err != nil {
		return fmt.Errorf("sync restored known_hosts directory: %w", err)
	}
	return nil
}

func knownHostPatternMatches(pattern, target string) bool {
	if strings.HasPrefix(pattern, "!") {
		return false
	}
	if !strings.HasPrefix(pattern, "|1|") {
		return knownhosts.Normalize(pattern) == target
	}
	parts := strings.Split(pattern, "|")
	if len(parts) != 4 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt) // #nosec G401 -- required by the OpenSSH known_hosts format.
	_, _ = mac.Write([]byte(target))
	return hmac.Equal(mac.Sum(nil), want)
}

func acceptedHostKeys(path string, data []byte, hostname string) ([]ssh.PublicKey, error) {
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	seen := map[string]bool{}
	accepted := []ssh.PublicKey{}
	for len(data) > 0 {
		_, _, key, _, rest, parseErr := ssh.ParseKnownHosts(data)
		if errors.Is(parseErr, io.EOF) {
			break
		}
		if parseErr != nil {
			return nil, fmt.Errorf("parse known_hosts: %w", parseErr)
		}
		data = rest
		fingerprint := HostKeyFingerprintSHA256(key)
		if seen[fingerprint] || callback(hostname, knownHostsRemoteAddr(nil), key) != nil {
			continue
		}
		seen[fingerprint] = true
		accepted = append(accepted, key)
	}
	return accepted, nil
}

func validateHostKeyReplacement(path, hostname string, replacement ssh.PublicKey, previous []ssh.PublicKey) error {
	callback, err := knownhosts.New(path)
	if err != nil {
		return fmt.Errorf("validate known_hosts replacement: %w", err)
	}
	if err := callback(hostname, knownHostsRemoteAddr(nil), replacement); err != nil {
		return fmt.Errorf("validate replacement host key: %w", err)
	}
	replacementFingerprint := HostKeyFingerprintSHA256(replacement)
	for _, key := range previous {
		if HostKeyFingerprintSHA256(key) == replacementFingerprint {
			continue
		}
		if err := callback(hostname, knownHostsRemoteAddr(nil), key); err == nil {
			return fmt.Errorf("replace host key: prior key remains trusted by a wildcard or unsupported known_hosts pattern")
		}
	}
	return nil
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
