package connectorports

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
)

type sessions struct {
	manager *console.Manager
}

func NewLiveConsoleSessions(manager *console.Manager) connectorapi.LiveConsoleSessions {
	if manager == nil {
		return nil
	}
	return sessions{manager: manager}
}

func (value sessions) List(ctx context.Context, runtimeID int64) ([]connectorapi.ConsoleRecord, error) {
	records, err := value.manager.List(ctx, runtimeID)
	if err != nil {
		return nil, mapError(err)
	}
	result := make([]connectorapi.ConsoleRecord, 0, len(records))
	for _, record := range records {
		result = append(result, gatewayRecord(record))
	}
	return result, nil
}

func (value sessions) Create(ctx context.Context, request connectorapi.LiveConsoleCreateRequest) (connectorapi.ConsoleRecord, error) {
	principal, err := corePrincipal(request.Principal)
	if err != nil {
		return connectorapi.ConsoleRecord{}, err
	}
	created, err := value.manager.Create(ctx, console.CreateRequest{
		RuntimeID: request.RuntimeID, Name: request.Name, CloseExisting: request.CloseExisting,
		Cols: request.Cols, Rows: request.Rows, WaitForStart: request.WaitForStart, Params: request.Params,
		Principal: principal, PrepareEnvironment: coreEnvironmentPreparer(request.PrepareEnvironment),
		EnvironmentContentHash: request.EnvironmentContentHash,
	})
	return gatewayRecord(created), mapError(err)
}

func (value sessions) Get(ctx context.Context, id int64) (connectorapi.ConsoleRecord, error) {
	record, err := value.manager.Get(ctx, id)
	return gatewayRecord(record), mapError(err)
}

func (value sessions) Input(ctx context.Context, principal connectorapi.Principal, id int64, data string) error {
	core, err := corePrincipal(principal)
	if err != nil {
		return err
	}
	return mapError(value.manager.Input(ctx, core, id, data))
}

func (value sessions) Close(ctx context.Context, principal connectorapi.Principal, id int64) error {
	core, err := corePrincipal(principal)
	if err != nil {
		return err
	}
	return mapError(value.manager.Close(ctx, core, id))
}

func (value sessions) RuntimeID(ctx context.Context, id int64) (int64, error) {
	runtimeID, err := value.manager.RuntimeID(ctx, id)
	return runtimeID, mapError(err)
}

func (value sessions) Attach(w http.ResponseWriter, r *http.Request, principal connectorapi.Principal, id int64, upgrade func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)) error {
	core, err := corePrincipal(principal)
	if err != nil {
		return err
	}
	return mapError(value.manager.Attach(w, r, core, id, upgrade))
}

func coreEnvironmentPreparer(preparer connectorapi.LiveConsoleEnvironmentPreparer) console.EnvironmentPreparer {
	if preparer == nil {
		return nil
	}
	return func(ctx context.Context, peerIdentity string) (console.EnvironmentPreparation, error) {
		prepared, err := preparer(ctx, peerIdentity)
		if err != nil {
			return console.EnvironmentPreparation{}, err
		}
		environment, ok := prepared.Environment.(*sessionenv.Envelope)
		if prepared.Environment != nil && !ok {
			if prepared.Release != nil {
				prepared.Release()
			}
			return console.EnvironmentPreparation{}, fmt.Errorf("unsupported live console environment implementation")
		}
		var finalize func(context.Context, console.SessionHandle) error
		if prepared.Finalize != nil {
			finalize = func(finalizeCtx context.Context, handle console.SessionHandle) error {
				return prepared.Finalize(finalizeCtx, connectorapi.ConsoleSessionHandle{ID: handle.ID, RuntimeID: handle.RuntimeID, Generation: handle.Generation})
			}
		}
		return console.EnvironmentPreparation{
			Environment: environment, Release: prepared.Release, PostValidate: prepared.PostValidate, Finalize: finalize,
		}, nil
	}
}

func gatewayRecord(record console.Record) connectorapi.ConsoleRecord {
	return connectorapi.ConsoleRecord{
		ID: record.ID, RuntimeID: record.RuntimeID, Generation: record.Generation,
		TargetName: record.TargetName, Name: record.Name, Status: record.Status,
		Transcript: record.Transcript, Error: record.Error, Cols: record.Cols, Rows: record.Rows,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, ClosedAt: record.ClosedAt,
		EnvironmentContentHash: record.EnvironmentContentHash,
	}
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, console.ErrNotFound):
		return connectorapi.ErrLiveConsoleNotFound
	case errors.Is(err, console.ErrSessionLimit):
		return connectorapi.ErrLiveConsoleSessionLimit
	case errors.Is(err, console.ErrClientLimit):
		return connectorapi.ErrLiveConsoleClientLimit
	case errors.Is(err, console.ErrInputTooLarge):
		return connectorapi.ErrLiveConsoleInputTooLarge
	default:
		var inactive console.InactiveError
		if errors.As(err, &inactive) {
			return connectorapi.LiveConsoleInactiveError{Status: inactive.Status, Detail: inactive.Detail}
		}
		return err
	}
}
