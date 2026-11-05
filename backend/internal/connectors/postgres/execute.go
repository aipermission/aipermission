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
		s.collector.Notice(notice.Message)
	}
}

func (s *postgresExecuteSession) Execute(ctx context.Context, sql string, collector *sqlexecute.Collector) error {
	s.collector = collector
	defer func() { s.collector = nil }()
	// Simple query lets Postgres parse dollar quotes, routines and statement lists.
	results := s.conn.PgConn().Exec(ctx, sql)
	defer results.Close()
	for results.NextResult() {
		if err := s.collectResult(results.ResultReader(), collector); err != nil {
			return err
		}
	}
	return results.Close()
}

func (s *postgresExecuteSession) collectResult(reader *pgconn.ResultReader, collector *sqlexecute.Collector) error {
	if fields := reader.FieldDescriptions(); fields != nil {
		columns := make([]string, len(fields))
		for index, field := range fields {
			columns[index] = field.Name
		}
		if err := collector.BeginResult(columns); err != nil {
			return err
		}
		defer collector.EndResult()
		rows := pgx.RowsFromResultReader(s.conn.TypeMap(), reader)
		defer rows.Close()
		for rows.Next() {
			if !collector.RetainingRows() {
				continue
			}
			values, err := rows.Values()
			if err != nil {
				return fmt.Errorf("decode postgres SQL result: %w", err)
			}
			for index, value := range values {
				values[index] = normalizePostgresValue(value)
			}
			// Retention limits never stop protocol consumption or later commands.
			if err := collector.AddRow(values); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	tag, err := reader.Close()
	if err == nil && tag.String() != "" {
		collector.Command(tag.String(), tag.RowsAffected())
	}
	return err
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
