package sqlserverconnector

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/golang-sql/sqlexp"
)

const executionTransactionSQL = "SELECT @@TRANCOUNT, XACT_STATE()"

type executionSession struct {
	conn      driver.Conn
	closeOnce sync.Once
	closeErr  error
}

func (s *executionSession) prepare(ctx context.Context, query string) (driver.Stmt, error) {
	return s.conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
}

func closeExecutionStatement(stmt driver.Stmt, err error) error {
	return errors.Join(err, stmt.Close())
}

func (s *executionSession) Execute(ctx context.Context, query string, collector *sqlexecute.Collector) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stmt, err := s.prepare(ctx, query)
	if err != nil {
		return err
	}
	defer func() { err = closeExecutionStatement(stmt, err) }()
	queryer, ok := stmt.(driver.StmtQueryContext)
	if !ok {
		return fmt.Errorf("SQL Server native contextual queries are unavailable")
	}
	messages := &sqlexp.ReturnMessage{}
	arg := driver.NamedValue{Ordinal: 1, Value: messages}
	if err := s.conn.(driver.NamedValueChecker).CheckNamedValue(&arg); !errors.Is(err, driver.ErrRemoveArgument) {
		return fmt.Errorf("SQL Server native message initialization failed: %v", err)
	}
	rows, err := queryer.QueryContext(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = s.Close(context.WithoutCancel(ctx))
		}
		err = errors.Join(err, rows.Close())
	}()
	return consumeExecutionMessages(ctx, cancel, rows, messages, collector)
}

func consumeExecutionMessages(ctx context.Context, cancel context.CancelFunc, rows driver.Rows, messages *sqlexp.ReturnMessage, collector *sqlexecute.Collector) (err error) {
	next, ok := rows.(driver.RowsNextResultSet)
	if !ok {
		return fmt.Errorf("SQL Server native multiple results are unavailable")
	}
	events, done := make(chan sqlexp.RawMessage), make(chan error, 1)
	sink := &executionSink{Collector: collector}
	go func() {
		err := pumpExecutionMessages(ctx, messages, sink, events)
		if err != nil {
			cancel()
		}
		done <- err
		close(events)
	}()
	defer func() {
		if cause := context.Cause(ctx); cause != nil && !errors.Is(err, cause) {
			err = errors.Join(err, cause)
		}
		cancel()
		err = errors.Join(err, <-done)
	}()
	for {
		var event sqlexp.RawMessage
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case message, more := <-events:
			if !more {
				return context.Cause(ctx)
			}
			event = message
		}
		switch event.(type) {
		case sqlexp.MsgNext:
			if err := consumeExecutionRows(ctx, rows, sink); err != nil {
				return err
			}
		case sqlexp.MsgNextResultSet:
			if err := next.NextResultSet(); err != nil {
				if errors.Is(err, io.EOF) {
					return context.Cause(ctx)
				}
				return err
			}
		}
	}
}

type executionSink struct {
	*sqlexecute.Collector
	sync.Mutex
}

// Notices can arrive while Next is blocked. Drain the driver's bounded message
// queue concurrently, but serialize all collector access and result advancement.
func pumpExecutionMessages(ctx context.Context, messages *sqlexp.ReturnMessage, sink *executionSink, events chan<- sqlexp.RawMessage) error {
	for ctx.Err() == nil {
		switch message := messages.Message(ctx).(type) {
		case sqlexp.MsgNext, sqlexp.MsgNextResultSet:
			select {
			case events <- message:
			case <-ctx.Done():
				return nil
			}
		case sqlexp.MsgRowsAffected:
			// The pinned driver also emits these for SELECT. Never infer DML
			// tags or aggregate an affected-row total from returned row counts.
			sink.Lock()
			sink.Command("server_done_count", message.Count)
			sink.Unlock()
		case sqlexp.MsgNotice:
			if message.Message != nil {
				sink.Lock()
				sink.Notice(message.Message.String())
				sink.Unlock()
			}
		case sqlexp.MsgError:
			if message.Error == nil {
				return fmt.Errorf("SQL Server returned an empty server error")
			}
			return message.Error
		default:
			return fmt.Errorf("SQL Server returned an unsupported native message")
		}
	}
	return nil
}

// This precedes connector Next, not the pinned driver's eager row allocations.
// Metadata rejection and retained output limits are not a driver heap guarantee.
func consumeExecutionRows(ctx context.Context, rows driver.Rows, sink *executionSink) error {
	columns := rows.Columns()
	types, ok := rows.(driver.RowsColumnTypeDatabaseTypeName)
	if !ok {
		return fmt.Errorf("SQL Server result type metadata is unavailable")
	}
	kinds := make([]string, len(columns))
	for i := range columns {
		kinds[i] = types.ColumnTypeDatabaseTypeName(i)
		if !supportedExecutionType(kinds[i]) {
			return unsupportedResultType()
		}
	}
	sink.Lock()
	err := sink.BeginResult(columns)
	sink.Unlock()
	if err != nil {
		return err
	}
	defer func() {
		sink.Lock()
		sink.EndResult()
		sink.Unlock()
	}()
	values := make([]driver.Value, len(columns))
	for {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if err := rows.Next(values); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := retainExecutionRow(sink, values, kinds); err != nil {
			return err
		}
		clear(values)
	}
}

func retainExecutionRow(sink *executionSink, values []driver.Value, kinds []string) error {
	sink.Lock()
	defer sink.Unlock()
	if !sink.RetainingRows() {
		return nil
	}
	normalized := make([]any, len(values))
	for i, value := range values {
		normalized[i] = normalizeValue(value, kinds[i])
	}
	return sink.AddRow(normalized)
}

func supportedExecutionType(kind string) bool {
	switch strings.ToUpper(kind) {
	case "IMAGE", "TEXT", "UNIQUEIDENTIFIER", "DATE", "TIME", "DATETIME2", "DATETIMEOFFSET", "TINYINT", "SMALLINT", "INT", "SMALLDATETIME", "REAL", "MONEY", "DATETIME", "FLOAT", "NTEXT", "BIT", "DECIMAL", "NUMERIC", "SMALLMONEY", "BIGINT", "VARBINARY", "VARCHAR", "BINARY", "CHAR", "TIMESTAMP", "NVARCHAR", "NCHAR":
		return true
	}
	return false
}

func (s *executionSession) Finish(ctx context.Context) (err error) {
	stmt, err := s.prepare(ctx, executionTransactionSQL)
	if err != nil {
		return err
	}
	defer func() { err = closeExecutionStatement(stmt, err) }()
	queryer, ok := stmt.(driver.StmtQueryContext)
	if !ok {
		return fmt.Errorf("SQL Server native contextual queries are unavailable")
	}
	rows, err := queryer.QueryContext(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	if len(rows.Columns()) != 2 {
		return fmt.Errorf("SQL Server transaction state is unavailable")
	}
	values := make([]driver.Value, 2)
	if err := rows.Next(values); err != nil {
		return err
	}
	count, countOK := values[0].(int64)
	state, stateOK := values[1].(int64)
	if !countOK || !stateOK || count != 0 || state != 0 {
		return fmt.Errorf("SQL Server script left an open or uncommittable transaction; connection teardown rolls it back, earlier commits may remain")
	}
	if err := rows.Next(values); !errors.Is(err, io.EOF) {
		return fmt.Errorf("SQL Server transaction state did not finish cleanly: %v", err)
	}
	if next, ok := rows.(driver.RowsNextResultSet); ok {
		if err := next.NextResultSet(); !errors.Is(err, io.EOF) {
			return fmt.Errorf("SQL Server transaction state returned unexpected results: %v", err)
		}
	}
	return context.Cause(ctx)
}

func (s *executionSession) Close(context.Context) error {
	s.closeOnce.Do(func() { s.closeErr = s.conn.Close() })
	return s.closeErr
}
