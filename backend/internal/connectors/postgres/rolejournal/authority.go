package rolejournal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// Authority binds every public target/admin revision, including project moves
// and credential changes. Ref is derived per profile, not target authority.
func Authority(runtime connectors.RuntimeContext) (Anchor, error) {
	target, profile := runtime.Target, runtime.Profile
	if target.ID < 1 || profile.ID < 1 || profile.TargetID != target.ID ||
		target.ConnectorKind == "" || target.ConnectorKind != profile.ConnectorKind {
		return Anchor{}, errors.New("managed Postgres role authority is not target/profile bound")
	}
	database, _ := target.Config["database"].(string)
	username, _ := profile.Public["username"].(string)
	targetDigest, err := targetAuthorityDigest(target)
	if err != nil {
		return Anchor{}, err
	}
	target.Ref = ""
	encoded, err := json.Marshal(struct {
		Target  connectors.TargetView            `json:"target"`
		Profile connectors.CredentialProfileView `json:"profile"`
	}{target, profile})
	if err != nil {
		return Anchor{}, errors.New("managed Postgres role authority cannot be serialized")
	}
	sum := sha256.Sum256(encoded)
	anchor := Anchor{TargetID: target.ID, AdminProfileID: profile.ID, TargetDigest: targetDigest,
		ContextDigest: hex.EncodeToString(sum[:]), DatabaseName: database, SuccessorName: username}
	if err := anchor.ValidateAuthority(); err != nil {
		return Anchor{}, err
	}
	return anchor, nil
}

func targetAuthorityDigest(target connectors.TargetView) (string, error) {
	database, _ := target.Config["database"].(string)
	if target.ID < 1 || target.ConnectorKind == "" || !validIdentifier(database) {
		return "", errors.New("managed Postgres target authority is invalid")
	}
	target.Ref = ""
	encoded, err := json.Marshal(target)
	if err != nil {
		return "", errors.New("managed Postgres target authority cannot be serialized")
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyTargetAuthority is only sufficient for reading already-confirmed local
// cleanup evidence. It never authorizes a connection or another remote action.
func VerifyTargetAuthority(target connectors.TargetView, expected Anchor) error {
	if err := expected.ValidateAuthority(); err != nil {
		return err
	}
	digest, err := targetAuthorityDigest(target)
	if err != nil {
		return err
	}
	database, _ := target.Config["database"].(string)
	if target.ID != expected.TargetID || digest != expected.TargetDigest || database != expected.DatabaseName {
		return errors.New("managed Postgres local target changed; operator reconciliation is required")
	}
	return nil
}

// VerifyAuthority must precede every remote connection and reconciliation. The
// persisted anchor is never substituted for the current credential's authority.
func VerifyAuthority(runtime connectors.RuntimeContext, expected Anchor) error {
	current, err := Authority(runtime)
	if err != nil {
		return err
	}
	return VerifyCurrentAuthority(current, expected)
}

// VerifyCurrentAuthority compares a freshly captured local authority with the
// saved anchor. Remote OIDs are checked separately under the catalog fence.
func VerifyCurrentAuthority(current, expected Anchor) error {
	if err := current.ValidateAuthority(); err != nil {
		return err
	}
	if err := expected.ValidateAuthority(); err != nil {
		return err
	}
	if current.TargetID != expected.TargetID || current.AdminProfileID != expected.AdminProfileID ||
		current.ContextDigest != expected.ContextDigest || current.TargetDigest != expected.TargetDigest || current.DatabaseName != expected.DatabaseName ||
		current.SuccessorName != expected.SuccessorName {
		return errors.New("managed Postgres local authority changed; operator reconciliation is required")
	}
	return nil
}
