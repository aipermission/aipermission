// Package keycleanup owns durable SSH authorized-key revocation evidence.
package keycleanup

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
)

type ProfileIdentity struct {
	ID             int64  `json:"id"`
	Revision       string `json:"revision"`
	SecretRevision string `json:"secret_revision"`
	PublicDigest   string `json:"public_digest"`
	KeyID          int64  `json:"key_id"`
	KeyRevision    string `json:"key_revision"`
}

type Identity struct {
	TargetID         int64             `json:"target_id"`
	TargetRevision   string            `json:"target_revision"`
	ConfigDigest     string            `json:"config_digest"`
	Host             string            `json:"host"`
	Port             int               `json:"port"`
	Username         string            `json:"username"`
	KeyDigest        string            `json:"key_digest"`
	HostFingerprints []string          `json:"host_fingerprints"`
	Profiles         []ProfileIdentity `json:"profiles"`
}

func NewIdentity(input Identity) (Identity, error) {
	input.Host = canonicalHost(input.Host)
	input.Username = strings.TrimSpace(input.Username)
	input.HostFingerprints = slices.Clone(input.HostFingerprints)
	slices.Sort(input.HostFingerprints)
	input.HostFingerprints = slices.Compact(input.HostFingerprints)
	input.Profiles = slices.Clone(input.Profiles)
	slices.SortFunc(input.Profiles, func(a, b ProfileIdentity) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return input, input.validate()
}

func Digest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (identity Identity) validate() error {
	if identity.TargetID < 1 || identity.TargetRevision == "" || !validDigest(identity.ConfigDigest) ||
		identity.Host == "" || identity.Host != canonicalHost(identity.Host) ||
		identity.Port < 1 || identity.Port > 65535 || identity.Username == "" ||
		identity.Username != strings.TrimSpace(identity.Username) || !validDigest(identity.KeyDigest) {
		return errors.New("invalid SSH key revocation identity")
	}
	if len(identity.HostFingerprints) == 0 || !slices.IsSorted(identity.HostFingerprints) {
		return errors.New("SSH key revocation requires canonical trusted host identities")
	}
	for index, fingerprint := range identity.HostFingerprints {
		decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(fingerprint, "SHA256:"))
		if !strings.HasPrefix(fingerprint, "SHA256:") || err != nil || len(decoded) != sha256.Size ||
			fingerprint != "SHA256:"+base64.RawStdEncoding.EncodeToString(decoded) ||
			(index > 0 && identity.HostFingerprints[index-1] == fingerprint) {
			return errors.New("invalid SSH key revocation host identity")
		}
	}
	if len(identity.Profiles) == 0 {
		return errors.New("SSH key revocation requires credential profiles")
	}
	for index, profile := range identity.Profiles {
		if profile.ID < 1 || profile.KeyID < 1 || profile.Revision == "" || profile.SecretRevision == "" ||
			profile.KeyRevision == "" || !validDigest(profile.PublicDigest) ||
			(index > 0 && identity.Profiles[index-1].ID >= profile.ID) {
			return errors.New("invalid SSH key revocation profile identity")
		}
	}
	return nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func canonicalHost(host string) string {
	host = strings.TrimSpace(host)
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return ip.String()
	}
	return strings.ToLower(host)
}

func (identity Identity) overlaps(other Identity) bool {
	if identity.Username != other.Username || identity.KeyDigest != other.KeyDigest {
		return false
	}
	if identity.TargetID == other.TargetID || (identity.Host == other.Host && identity.Port == other.Port) {
		return true
	}
	for _, fingerprint := range identity.HostFingerprints {
		if slices.Contains(other.HostFingerprints, fingerprint) {
			return true
		}
	}
	return false
}
