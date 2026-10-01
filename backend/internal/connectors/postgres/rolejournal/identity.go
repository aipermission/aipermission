// Package rolejournal owns durable evidence for managed PostgreSQL roles.
package rolejournal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Anchor binds both the local authority and the remote ownership successor.
// ClusterID is decimal text: PostgreSQL system identifiers can exceed 2^53.
type Anchor struct {
	TargetID       int64  `json:"target_id"`
	ContextDigest  string `json:"context_digest"`
	TargetDigest   string `json:"target_digest"`
	AdminProfileID int64  `json:"admin_profile_id"`
	ClusterID      string `json:"cluster_id"`
	DatabaseOID    uint32 `json:"database_oid"`
	DatabaseName   string `json:"database_name"`
	SuccessorOID   uint32 `json:"successor_oid"`
	SuccessorName  string `json:"successor_name"`
}

type Intent struct {
	Anchor      Anchor `json:"anchor"`
	RoleName    string `json:"role_name"`
	OperationID string `json:"operation_id"`
}

// Marker is never inferred from a role name or accepted as sole ownership proof.
func (intent Intent) Marker() string { return "aipermission-provision:" + intent.OperationID }

func (anchor Anchor) Validate() error {
	if err := anchor.ValidateAuthority(); err != nil {
		return err
	}
	cluster, err := strconv.ParseUint(anchor.ClusterID, 10, 64)
	if err != nil || cluster == 0 || strconv.FormatUint(cluster, 10) != anchor.ClusterID ||
		anchor.DatabaseOID == 0 || anchor.SuccessorOID == 0 {
		return errors.New("invalid managed Postgres role anchor")
	}
	return nil
}

// ValidateAuthority checks the local snapshot and requested exact names before
// remote discovery fills cluster/database/role OIDs. It does not verify a server.
func (anchor Anchor) ValidateAuthority() error {
	if anchor.TargetID < 1 || anchor.AdminProfileID < 1 || !validHex(anchor.ContextDigest, 32) || !validHex(anchor.TargetDigest, 32) ||
		!validIdentifier(anchor.DatabaseName) || !validIdentifier(anchor.SuccessorName) {
		return errors.New("invalid managed Postgres role authority")
	}
	return nil
}

func (intent Intent) validate() error {
	if err := intent.Anchor.Validate(); err != nil {
		return err
	}
	if !validIdentifier(intent.RoleName) || !validHex(intent.OperationID, 16) ||
		intent.RoleName == intent.Anchor.SuccessorName {
		return errors.New("invalid managed Postgres role intent")
	}
	return nil
}

func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 63 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size && hex.EncodeToString(decoded) == value
}

func intentDigest(intent Intent) string {
	encoded, _ := json.Marshal(intent) // All fields have fixed JSON-serializable types.
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// Overlap must survive local endpoint changes and aliases to the same cluster.
func (intent Intent) overlaps(other Intent) bool {
	return intent.RoleName == other.RoleName &&
		(intent.Anchor.TargetID == other.Anchor.TargetID || intent.Anchor.ClusterID == other.Anchor.ClusterID)
}
