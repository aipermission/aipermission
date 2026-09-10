package commandrequests

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type WorkspaceRuntimeDependencies struct {
	Database          Database
	Vault             *vault.Vault
	WorkspaceID       string
	Redact            Redactor
	Sessions          ActiveSessions
	BackgroundTimeout time.Duration
}

func NewWorkspaceRuntime(dependencies WorkspaceRuntimeDependencies) (*Runtime, error) {
	if dependencies.Database == nil || dependencies.Vault == nil ||
		strings.TrimSpace(dependencies.WorkspaceID) == "" {
		return nil, ErrRuntimeUnavailable
	}
	return NewRuntime(RuntimeDependencies{
		Store:             NewStore(dependencies.Database),
		Codec:             workspaceCommandCodec{vault: dependencies.Vault, workspaceID: dependencies.WorkspaceID},
		Projection:        commandHistoryProjection{},
		Redact:            dependencies.Redact,
		Sessions:          dependencies.Sessions,
		BackgroundTimeout: dependencies.BackgroundTimeout,
	})
}

type workspaceCommandCodec struct {
	vault       *vault.Vault
	workspaceID string
}

func (c workspaceCommandCodec) Seal(id int64, command string) (string, error) {
	return recordcrypto.EncryptJSON(c.vault, c.workspaceID, recordcrypto.CommandRequest, id, command)
}

func (c workspaceCommandCodec) Open(id int64, sealed string) (string, error) {
	var command string
	if err := recordcrypto.DecryptJSON(
		c.vault, c.workspaceID, recordcrypto.CommandRequest, id, sealed, &command,
	); err != nil {
		return "", err
	}
	return command, nil
}

type commandHistoryProjection struct{}

func (commandHistoryProjection) SyncCommandRequest(ctx context.Context, executor Executor, id int64) error {
	if err := history.SyncCommandRequestWithExecutor(ctx, executor, id); err != nil {
		return fmt.Errorf("sync command request history: %w", err)
	}
	return nil
}
