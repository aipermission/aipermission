// Package executionprincipal defines the authenticated actor attached to
// runtime-backed connector work. Its zero value is deliberately invalid.
package executionprincipal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type Kind string

const (
	KindLocalOperator Kind = "local_operator"
	KindMCPToken      Kind = "mcp_token"
)

var ErrInvalid = errors.New("execution principal is required")

type Principal struct {
	Kind              Kind
	TokenID           int64
	WorkspaceID       string
	RuntimeInstanceID string
}

func LocalOperator(workspaceID string, runtimeInstanceID string) (Principal, error) {
	return newPrincipal(KindLocalOperator, 0, workspaceID, runtimeInstanceID)
}

func MCPToken(tokenID int64, workspaceID string, runtimeInstanceID string) (Principal, error) {
	return newPrincipal(KindMCPToken, tokenID, workspaceID, runtimeInstanceID)
}

func (p Principal) Validate() error {
	if p.Kind != KindLocalOperator && p.Kind != KindMCPToken {
		return ErrInvalid
	}
	if strings.TrimSpace(p.WorkspaceID) == "" || strings.TrimSpace(p.RuntimeInstanceID) == "" {
		return ErrInvalid
	}
	if p.Kind == KindMCPToken && p.TokenID < 1 {
		return ErrInvalid
	}
	if p.Kind == KindLocalOperator && p.TokenID != 0 {
		return ErrInvalid
	}
	return nil
}

func (p Principal) IsLocalOperator() bool {
	return p.Validate() == nil && p.Kind == KindLocalOperator
}

func (p Principal) SameRuntime(other Principal) bool {
	return p.Validate() == nil &&
		other.Validate() == nil &&
		p.WorkspaceID == other.WorkspaceID &&
		p.RuntimeInstanceID == other.RuntimeInstanceID
}

func (p Principal) String() string {
	if p.Kind == KindMCPToken {
		return fmt.Sprintf("%s:%d", p.Kind, p.TokenID)
	}
	return string(p.Kind)
}

func NewRuntimeInstanceID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate runtime instance id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func newPrincipal(kind Kind, tokenID int64, workspaceID string, runtimeInstanceID string) (Principal, error) {
	principal := Principal{
		Kind:              kind,
		TokenID:           tokenID,
		WorkspaceID:       strings.TrimSpace(workspaceID),
		RuntimeInstanceID: strings.TrimSpace(runtimeInstanceID),
	}
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}
