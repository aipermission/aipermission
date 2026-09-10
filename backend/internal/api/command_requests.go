package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type commandRequestInsert = commandrequests.Insert
type preparedCommandRequestInsert = commandrequests.PreparedInsert

type commandRequestCodec struct{ runtime *databaseRuntime }

func (c commandRequestCodec) Seal(id int64, command string) (string, error) {
	return recordcrypto.EncryptJSON(
		c.runtime.vault, c.runtime.workspaceUUID, recordcrypto.CommandRequest, id, command,
	)
}

func (c commandRequestCodec) Open(id int64, sealed string) (string, error) {
	var command string
	if err := recordcrypto.DecryptJSON(
		c.runtime.vault, c.runtime.workspaceUUID, recordcrypto.CommandRequest, id, sealed, &command,
	); err != nil {
		return "", err
	}
	return command, nil
}

type commandRequestProjection struct{}

func (commandRequestProjection) SyncCommandRequest(
	ctx context.Context,
	executor commandrequests.Executor,
	id int64,
) error {
	return history.SyncCommandRequestWithExecutor(ctx, executor, id)
}

func (s *Server) commandRequestRuntime(runtime *databaseRuntime) (*commandrequests.Runtime, error) {
	if s == nil || runtime == nil || runtime.database == nil || runtime.vault == nil || runtime.consoleSessions == nil {
		return nil, commandrequests.ErrRuntimeUnavailable
	}
	owner, err := commandrequests.NewRuntime(commandrequests.RuntimeDependencies{
		Store:      commandrequests.NewStore(runtime.database),
		Codec:      commandRequestCodec{runtime: runtime},
		Projection: commandRequestProjection{},
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Sessions:          runtime.consoleSessions,
		BackgroundTimeout: mcpBackgroundCommandTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize command request runtime: %w", err)
	}
	return owner, nil
}

func (s *Server) commandRequestHTTPScope(w http.ResponseWriter) (*commandrequests.Store, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	return commandrequests.NewStore(runtime.database), true
}

func (s *Server) insertCommandRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID int64,
	runtimeID int64,
	command string,
	reason string,
	status string,
) (int64, error) {
	return s.insertCommandRequestWithOptions(ctx, runtime, commandRequestInsert{
		TokenID: &tokenID, RuntimeID: runtimeID, Source: commandRequestSourceMCP,
		Command: command, Reason: reason, Status: status,
	})
}

func (s *Server) insertCommandRequestWithOptions(
	ctx context.Context,
	runtime *databaseRuntime,
	request commandRequestInsert,
) (int64, error) {
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return 0, err
	}
	return owner.Insert(ctx, request)
}

func (s *Server) prepareCommandRequestInsert(
	ctx context.Context,
	runtime *databaseRuntime,
	request commandRequestInsert,
) (preparedCommandRequestInsert, error) {
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return preparedCommandRequestInsert{}, err
	}
	return owner.Prepare(ctx, request)
}

func (s *Server) insertCommandRequestWithExecutor(
	ctx context.Context,
	runtime *databaseRuntime,
	executor commandrequests.Executor,
	request preparedCommandRequestInsert,
) (int64, error) {
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return 0, err
	}
	return owner.InsertPrepared(ctx, executor, request)
}

func (s *Server) commandRequestExecutionCommand(
	ctx context.Context,
	runtime *databaseRuntime,
	id int64,
) (string, error) {
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return "", err
	}
	return owner.ExecutionCommand(ctx, id)
}

func (s *Server) finishActiveCommandRequest(
	runtime *databaseRuntime,
	requestID int64,
	principal executionprincipal.Principal,
	handle console.SessionHandle,
) {
	owner, err := s.commandRequestRuntime(runtime)
	if err == nil {
		owner.FinishActive(requestID, principal, handle)
	}
}

func (s *Server) setCommandRequestSession(
	ctx context.Context,
	runtime *databaseRuntime,
	id int64,
	sessionID int64,
) error {
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return err
	}
	return owner.SetSession(ctx, id, sessionID)
}

func (s *Server) finishCommandRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	id int64,
	status string,
	sessionID int64,
	stdout string,
	stderr string,
	exitCode int,
	errorText string,
) error {
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return err
	}
	return owner.Finish(ctx, commandrequests.Completion{
		ID: id, Status: status, SessionID: sessionID, Stdout: stdout, Stderr: stderr,
		ExitCode: exitCode, Error: errorText,
	})
}

func (s *Server) cancelRunningCommandRequests(
	ctx context.Context,
	runtime *databaseRuntime,
	errorText string,
) error {
	if runtime == nil || runtime.database == nil {
		return nil
	}
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return err
	}
	return owner.CancelRunning(ctx, errorText)
}

func (s *Server) cancelRunningCommandRequestsForSession(
	ctx context.Context,
	runtime *databaseRuntime,
	sessionID int64,
	errorText string,
) error {
	if runtime == nil || runtime.database == nil || sessionID < 1 {
		return nil
	}
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return err
	}
	return owner.CancelRunningForSession(ctx, sessionID, errorText)
}

func (s *Server) cancelRunningCommandRequestsForServer(
	ctx context.Context,
	runtime *databaseRuntime,
	runtimeID int64,
	errorText string,
) (int64, error) {
	if runtime == nil || runtime.database == nil || runtimeID < 1 {
		return 0, nil
	}
	owner, err := s.commandRequestRuntime(runtime)
	if err != nil {
		return 0, err
	}
	return owner.CancelRunningForRuntime(ctx, runtimeID, errorText)
}
