package postgresconnector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

func validateExecutionAction(runtime connectors.RuntimeContext, action connectors.PreparedAction) error {
	target, profile := runtime.Target, runtime.Profile
	if target.ConnectorKind != Kind || profile.ConnectorKind != Kind || action.ConnectorKind != Kind ||
		target.ID < 1 || profile.ID < 1 || profile.TargetID != target.ID || profile.Kind != "username_password" ||
		action.ProfileID != profile.ID || action.TargetRef != target.Ref ||
		target.Ref != connectors.FormatTargetRef(Kind, target.ID, profile.ID) {
		return fmt.Errorf("postgres action target/profile identity mismatch")
	}
	wantRisk := connectors.RiskRead
	switch action.ActionName {
	case ActionExecuteSQL:
		wantRisk = connectors.RiskWrite
	case ActionGetSchemas, ActionGetTables, ActionDescribeTable, ActionQueryReadonly:
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAction, action.ActionName)
	}
	if action.Risk != wantRisk {
		return fmt.Errorf("postgres action risk mismatch")
	}
	if action.ActionName == ActionQueryReadonly {
		return validateReadonlySQL(payloadString(action.Payload, "sql"))
	}
	return nil
}

func executeSQL(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	// Revalidate persisted input without trimming or splitting the approved script.
	prepared, err := sqlexecute.Prepare(connectors.ActionRequest{
		Target: runtime.Target, Profile: runtime.Profile, ActionName: action.ActionName, Input: action.Payload,
	}, Kind, Label)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	_, seconds, err := sqlexecute.Limits(prepared.Payload)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	sql := prepared.Payload["sql"].(string)
	return sqlexecute.Run(ctx, prepared, []string{sql}, func(ctx context.Context) (sqlexecute.Session, error) {
		session := &postgresExecuteSession{}
		conn, err := connect(ctx, runtime, postgresSessionOptions{
			timeout: time.Duration(seconds) * time.Second, onNotice: session.notice, bindLifetime: true,
		})
		if err != nil {
			return nil, err
		}
		session.conn = conn
		return session, nil
	})
}

type postgresExecuteSession struct {
	conn      *pgx.Conn
	collector *sqlexecute.Collector
}

func (s *postgresExecuteSession) notice(_ *pgconn.PgConn, notice *pgconn.Notice) {
	if s.collector != nil {
		if s.supportsResultText(notice.Message) {
			s.collector.Notice(notice.Message)
		} else {
			s.collector.Notice("PostgreSQL notice omitted: unsupported client encoding or invalid UTF-8 text.")
		}
	}
}

func (s *postgresExecuteSession) Execute(ctx context.Context, sql string, collector *sqlexecute.Collector) (err error) {
	s.collector = collector
	defer func() { s.collector = nil }()
	// Simple query lets Postgres parse dollar quotes, routines and statement lists.
	conn := s.conn.PgConn()
	if err := conn.SyncConn(ctx); err != nil {
		return err
	}
	defer func() {
		// ReceiveMessage may notice an already canceled context without entering
		// pgconn's async-close path. Preserve native cancellation in that case.
		if err != nil && ctx.Err() != nil && !conn.IsClosed() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			err = errors.Join(err, conn.CancelRequest(cleanup))
		}
	}()
	conn.Frontend().SendQuery(&pgproto3.Query{String: sql})
	if err := conn.Frontend().Flush(); err != nil {
		return err
	}
	return s.receiveExecution(ctx, collector)
}

func (s *postgresExecuteSession) Finish(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.conn.IsClosed() || s.conn.PgConn().TxStatus() != 'I' {
		return fmt.Errorf("postgres SQL session did not finish idle; transaction outcome requires reconciliation")
	}
	return nil
}

func (s *postgresExecuteSession) Close(ctx context.Context) error {
	// pgx can mark a failed connection closed before its async socket cleanup.
	// Tunnel sockets may not implement deadlines; cleanup must still be bounded.
	socket := connectors.BindNetworkConnectionContext(ctx, s.conn.PgConn().Conn())
	defer socket.Close()
	var rollbackErr error
	if !s.conn.IsClosed() && s.conn.PgConn().TxStatus() != 'I' {
		_, rollbackErr = s.conn.Exec(ctx, "ROLLBACK")
	}
	closeErr := s.conn.Close(ctx)
	select {
	case <-s.conn.PgConn().CleanupDone():
		return errors.Join(rollbackErr, closeErr)
	case <-ctx.Done():
		return errors.Join(rollbackErr, closeErr, ctx.Err())
	}
}
